package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const appVersion = "1.3.19"
const assetZip = "Malowar_News_Agent_Update.zip"
const assetHash = "Malowar_News_Agent_Update.zip.sha256"

var safeRepo = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,99}/[a-zA-Z0-9][a-zA-Z0-9_.-]{0,99}$`)
var hex64 = regexp.MustCompile(`^[a-fA-F0-9]{64}$`)

func validUpdateRepo(s string) bool { return safeRepo.MatchString(s) && !strings.Contains(s, "..") }
func compareVersions(a, b string) int {
	a = strings.TrimPrefix(strings.TrimSpace(a), "v")
	b = strings.TrimPrefix(strings.TrimSpace(b), "v")
	aa, bb := strings.Split(a, "."), strings.Split(b, ".")
	if len(aa) != 3 || len(bb) != 3 {
		return 0
	}
	for i := 0; i < 3; i++ {
		x, e1 := strconv.Atoi(aa[i])
		y, e2 := strconv.Atoi(bb[i])
		if e1 != nil || e2 != nil {
			return 0
		}
		if x < y {
			return -1
		}
		if x > y {
			return 1
		}
	}
	return 0
}

type releaseAsset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
	Size int64  `json:"size"`
}
type releaseInfo struct {
	Tag        string         `json:"tag_name"`
	Name       string         `json:"name"`
	Body       string         `json:"body"`
	Prerelease bool           `json:"prerelease"`
	Draft      bool           `json:"draft"`
	Assets     []releaseAsset `json:"assets"`
}
type updateInfo struct {
	Configured bool   `json:"configured"`
	Available  bool   `json:"available"`
	Current    string `json:"current"`
	Latest     string `json:"latest,omitempty"`
	Notes      string `json:"notes,omitempty"`
	Repo       string `json:"repo,omitempty"`
	Ready      bool   `json:"ready"`
	Message    string `json:"message,omitempty"`
}

func githubAssetURL(s, repo string) bool {
	u, err := url.Parse(s)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Hostname() != "github.com" {
		return false
	}
	return strings.HasPrefix(u.EscapedPath(), "/"+repo+"/releases/download/")
}
func trustedGitRedirect(raw string) bool {
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != "https" || u.User != nil {
		return false
	}
	h := strings.ToLower(u.Hostname())
	return h == "github.com" || h == "api.github.com" || h == "release-assets.githubusercontent.com" || h == "objects.githubusercontent.com"
}

var releaseClient = &http.Client{Timeout: 60 * time.Second, CheckRedirect: func(r *http.Request, via []*http.Request) error {
	if len(via) > 5 || !trustedGitRedirect(r.URL.String()) {
		return errors.New("Недоверенный адрес обновления")
	}
	return nil
}}

func fetchRelease(ctx context.Context, repo string) (releaseInfo, error) {
	if !validUpdateRepo(repo) {
		return releaseInfo{}, errors.New("Укажи репозиторий релизов в формате owner/repo в настройках")
	}
	uri := "https://api.github.com/repos/" + repo + "/releases/latest"
	req, e := http.NewRequestWithContext(ctx, "GET", uri, nil)
	if e != nil {
		return releaseInfo{}, e
	}
	req.Header.Set("User-Agent", "MalowarNewsAgent/1.3.19")
	req.Header.Set("Accept", "application/vnd.github+json")
	res, e := releaseClient.Do(req)
	if e != nil {
		return releaseInfo{}, e
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return releaseInfo{}, fmt.Errorf("GitHub HTTP %d: проверь репозиторий и наличие публичных Releases", res.StatusCode)
	}
	var rel releaseInfo
	if e = json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&rel); e != nil {
		return rel, e
	}
	if rel.Draft || rel.Prerelease {
		return rel, errors.New("Тестовые релизы не устанавливаются автоматически")
	}
	return rel, nil
}
func findUpdateAssets(rel releaseInfo, repo string) (releaseAsset, releaseAsset, error) {
	var z, h releaseAsset
	for _, a := range rel.Assets {
		if !githubAssetURL(a.URL, repo) {
			continue
		}
		switch a.Name {
		case assetZip:
			z = a
		case assetHash:
			h = a
		}
	}
	if z.Name == "" || h.Name == "" || z.Size <= 0 || z.Size > 80<<20 || h.Size > 1024 {
		return z, h, errors.New("В релизе нет архива обновления и контрольной суммы SHA256")
	}
	return z, h, nil
}
func (a *app) updateCheck(ctx context.Context) (updateInfo, error) {
	a.mu.Lock()
	repo := strings.TrimSpace(a.state.Config.UpdateRepo)
	a.mu.Unlock()
	info := updateInfo{Current: appVersion, Repo: repo, Configured: repo != ""}
	if repo == "" {
		info.Message = "В настройках пока не указан официальный GitHub-репозиторий обновлений"
		return info, nil
	}
	rel, e := fetchRelease(ctx, repo)
	if e != nil {
		return info, e
	}
	info.Latest = strings.TrimPrefix(rel.Tag, "v")
	info.Available = compareVersions(appVersion, info.Latest) < 0
	info.Notes = rel.Body
	if len(info.Notes) > 1200 {
		info.Notes = info.Notes[:1200]
	}
	if info.Available {
		_, _, e = findUpdateAssets(rel, repo)
		info.Ready = e == nil
		if e != nil {
			info.Message = e.Error()
		}
	} else {
		info.Message = "Установлена актуальная версия"
	}
	return info, nil
}
func downloadGitAsset(ctx context.Context, link, repo string, max int64) ([]byte, error) {
	if !githubAssetURL(link, repo) {
		return nil, errors.New("Адрес обновления не принадлежит заданному GitHub Release")
	}
	req, e := http.NewRequestWithContext(ctx, "GET", link, nil)
	if e != nil {
		return nil, e
	}
	req.Header.Set("User-Agent", "MalowarNewsAgent/1.3.19")
	res, e := releaseClient.Do(req)
	if e != nil {
		return nil, e
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("GitHub asset HTTP %d", res.StatusCode)
	}
	return io.ReadAll(io.LimitReader(res.Body, max+1))
}
func (a *app) beginUpdate(ctx context.Context) error {
	exe, e := os.Executable()
	if e != nil {
		return e
	}
	dir := filepath.Dir(exe)
	if _, e := os.Stat(filepath.Join(dir, ".mna-install.json")); e != nil {
		return errors.New("Автообновление доступно только для установленной версии, не Portable")
	}
	updater := filepath.Join(dir, "Malowar_News_Updater.exe")
	if _, e := os.Stat(updater); e != nil {
		return errors.New("Не найден обновлятор. Переустанови версию 1.3.7")
	}
	a.mu.Lock()
	repo := a.state.Config.UpdateRepo
	a.mu.Unlock()
	rel, e := fetchRelease(ctx, repo)
	if e != nil {
		return e
	}
	if compareVersions(appVersion, rel.Tag) >= 0 {
		return errors.New("Новая версия пока не опубликована")
	}
	z, h, e := findUpdateAssets(rel, repo)
	if e != nil {
		return e
	}
	hashData, e := downloadGitAsset(ctx, h.URL, repo, 1024)
	if e != nil {
		return e
	}
	fields := strings.Fields(string(hashData))
	if len(fields) == 0 {
		return errors.New("Пустой файл SHA256")
	}
	want := strings.ToLower(fields[0])
	if !hex64.MatchString(want) {
		return errors.New("Некорректная контрольная сумма релиза")
	}
	archive, e := downloadGitAsset(ctx, z.URL, repo, 80<<20)
	if e != nil {
		return e
	}
	if len(archive) > 80<<20 {
		return errors.New("Архив превышает допустимый размер")
	}
	actual := sha256.Sum256(archive)
	if hex.EncodeToString(actual[:]) != want {
		return errors.New("Контрольная сумма обновления не совпадает. Установка отменена")
	}
	updateDir := filepath.Join(a.dataDir, "updates")
	if e = os.MkdirAll(updateDir, 0700); e != nil {
		return e
	}
	pkg := filepath.Join(updateDir, "pending-"+strings.TrimPrefix(rel.Tag, "v")+".zip")
	if e = os.WriteFile(pkg, archive, 0600); e != nil {
		return e
	}
	cmd := exec.Command(updater, "--dir", dir, "--zip", pkg, "--sha256", want, "--version", strings.TrimPrefix(rel.Tag, "v"))
	cmd.Dir = dir
	if e = cmd.Start(); e != nil {
		return fmt.Errorf("Не удалось запустить обновлятор: %w", e)
	}
	_ = cmd.Process.Release()
	return nil
}
