package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Only assets returned by Steam's own catalog or Workshop API are considered.
// Never scrape random third-party news-site images or auto-generate them as screenshots.
func trustedSteamImage(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil {
		return false
	}
	h := strings.ToLower(u.Hostname())
	if h == "steamstatic.com" || strings.HasSuffix(h, ".steamstatic.com") || h == "steamcommunity-a.akamaihd.net" {
		return true
	}
	if h == "steamusercontent.com" || strings.HasSuffix(h, ".steamusercontent.com") {
		return true
	}
	return false
}

func collectSteamImages(ctx context.Context, id int) ([]string, error) {
	if id <= 0 {
		return nil, errors.New("Нет Steam AppID")
	}
	uri := "https://store.steampowered.com/api/appdetails?appids=" + strconv.Itoa(id) + "&l=english&cc=us"
	b, err := get(ctx, uri, nil)
	if err != nil {
		return nil, err
	}
	var wrapper map[string]struct {
		Success bool `json:"success"`
		Data    struct {
			Screenshots []struct {
				Full string `json:"path_full"`
			} `json:"screenshots"`
			Header string `json:"header_image"`
		} `json:"data"`
	}
	if err := json.Unmarshal(b, &wrapper); err != nil {
		return nil, err
	}
	result, ok := wrapper[strconv.Itoa(id)]
	if !ok || !result.Success {
		return nil, errors.New("Нет открытых изображений в Steam Store")
	}
	out := make([]string, 0, 9)
	seen := map[string]bool{}
	// Official library hero art is higher-resolution than the small Steam Store header.
	// It is attempted first but may not exist for every Steam AppID.
	hero := fmt.Sprintf("https://cdn.akamai.steamstatic.com/steam/apps/%d/library_hero.jpg", id)
	if trustedSteamImage(hero) {
		out = append(out, hero)
		seen[hero] = true
	}
	if trustedSteamImage(result.Data.Header) && !seen[result.Data.Header] {
		out = append(out, result.Data.Header)
		seen[result.Data.Header] = true
	}
	for _, s := range result.Data.Screenshots {
		if trustedSteamImage(s.Full) && !seen[s.Full] {
			out = append(out, s.Full)
			seen[s.Full] = true
		}
		if len(out) >= 10 {
			break
		}
	}
	return out, nil
}

func collectWorkshopImages(ctx context.Context, id string) ([]string, error) {
	if id == "" || strings.Trim(id, "0123456789") != "" {
		return nil, errors.New("Неверный Workshop ID")
	}
	vals := url.Values{"itemcount": {"1"}, "publishedfileids[0]": {id}}
	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.steampowered.com/ISteamRemoteStorage/GetPublishedFileDetails/v1/", strings.NewReader(vals.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("Workshop HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	var data struct {
		Response struct {
			Details []struct {
				Result   int    `json:"result"`
				Preview  string `json:"preview_url"`
				Previews []struct {
					URL        string `json:"url"`
					PreviewURL string `json:"preview_url"`
					Type       int    `json:"preview_type"`
				} `json:"previews"`
			} `json:"publishedfiledetails"`
		} `json:"response"`
	}
	if err := json.Unmarshal(b, &data); err != nil {
		return nil, err
	}
	if len(data.Response.Details) == 0 || data.Response.Details[0].Result != 1 {
		return nil, errors.New("Мод недоступен в Steam Workshop")
	}
	detail := data.Response.Details[0]
	urls := []string{detail.Preview}
	for _, p := range detail.Previews {
		if p.Type != 0 {
			continue
		}
		if p.URL != "" {
			urls = append(urls, p.URL)
		} else {
			urls = append(urls, p.PreviewURL)
		}
	}
	out := []string{}
	seen := map[string]bool{}
	for _, raw := range urls {
		if trustedSteamImage(raw) && !seen[raw] {
			out = append(out, raw)
			seen[raw] = true
		}
		if len(out) >= 7 {
			break
		}
	}
	return out, nil
}

// Legacy wrapper retained for compatibility with existing integration callers.
// All choices now use the same news/DLC/workshop-aware planner.
func (a *app) mediaCandidates(ctx context.Context, item Item) ([]string, error) {
	choices, _, err := a.mediaPlan(ctx, item)
	out := make([]string, 0, len(choices))
	for _, c := range choices {
		out = append(out, c.URL)
	}
	return out, err
}

func saveTrustedImage(ctx context.Context, raw, dir string, index int) (string, error) {
	if !trustedSteamImage(raw) {
		return "", errors.New("Недоверенный адрес изображения")
	}
	// A private client enforces a trusted domain on every redirect as well.
	safeClient := &http.Client{Timeout: 12 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 3 || !trustedSteamImage(req.URL.String()) {
			return errors.New("Недопустимое перенаправление изображения")
		}
		return nil
	}}
	req, err := http.NewRequestWithContext(ctx, "GET", raw, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "MalowarNewsAgent/1.2 (official Steam image retrieval)")
	resp, err := safeClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if resp.ContentLength > 12<<20 {
		return "", errors.New("Изображение слишком большое")
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, (12<<20)+1))
	if err != nil {
		return "", err
	}
	if len(b) > 12<<20 {
		return "", errors.New("Изображение слишком большое")
	}
	kind := http.DetectContentType(b)
	ext := ""
	if kind == "image/jpeg" {
		ext = ".jpg"
	} else if kind == "image/png" {
		ext = ".png"
	} else {
		return "", errors.New("Только JPG и PNG")
	}
	dimensions, _, err := image.DecodeConfig(bytes.NewReader(b))
	if err != nil {
		return "", err
	}
	if dimensions.Width < 430 || dimensions.Height < 190 {
		return "", errors.New("Слишком маленькое изображение")
	}
	if err = os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	h := sha256.Sum256(b)
	path := filepath.Join(dir, fmt.Sprintf("steam-%02d-%x%s", index, h[:6], ext))
	if err = os.WriteFile(path, b, 0600); err != nil {
		return "", err
	}
	return path, nil
}

func (a *app) prepareImages(ctx context.Context, id string) error {
	return a.prepareImagesMode(ctx, id, false)
}

// Explicit rebuild, never automatic. Old media files remain on disk as a safety net.
func (a *app) rebuildThematicImages(ctx context.Context, id string) error {
	return a.prepareImagesMode(ctx, id, true)
}

func (a *app) prepareImagesMode(ctx context.Context, id string, reset bool) error {
	a.mu.Lock()
	original := a.getItemLocked(id)
	if original == nil {
		a.mu.Unlock()
		return errors.New("Новость не найдена")
	}
	if original.State == "published" || original.State == "publishing" || original.State == "uncertain" || original.State == "approved" {
		a.mu.Unlock()
		return errors.New("Для этой записи менять изображения нельзя")
	}
	if original.MediaStatus == "working" {
		a.mu.Unlock()
		return errors.New("Подбор уже выполняется")
	}
	original.MediaStatus = "working"
	original.MediaError = ""
	cloned := *original
	cloned.Images = append([]string(nil), original.Images...)
	cloned.MediaSources = append([]string(nil), original.MediaSources...)
	cloned.MediaLabels = append([]string(nil), original.MediaLabels...)
	if reset {
		cloned.Images = nil
		cloned.MediaSources = nil
		cloned.MediaLabels = nil
	}
	a.mu.Unlock()

	candidates, focus, lookupErr := a.mediaPlan(ctx, cloned)
	newly := []string{}
	sources := []string{}
	labels := []string{}
	seen := map[string]bool{}
	for _, old := range cloned.MediaSources {
		if old != "" {
			seen[old] = true
		}
	}
	// Download in parallel, but preserve Steam's original order (hero, header,
	// screenshots). A slow or missing library_hero.jpg must not block screenshots.
	// A bounded worker count prevents creating an excessive number of requests.
	type candidate struct {
		source string
		label  string
		index  int
	}
	jobs := make([]candidate, 0, len(candidates))
	for i, raw := range candidates {
		if seen[raw.URL] {
			continue
		}
		seen[raw.URL] = true
		jobs = append(jobs, candidate{source: raw.URL, index: i, label: raw.Label})
	}
	type download struct {
		path   string
		source string
		label  string
	}
	results := make([]download, len(jobs))
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	dir := filepath.Join(a.dataDir, "images", safeID(id))
	if reset {
		dir = filepath.Join(dir, fmt.Sprintf("rebuild-%d", time.Now().UnixNano()))
	}
	for i, job := range jobs {
		wg.Add(1)
		go func(i int, job candidate) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()
			path, err := saveTrustedImage(ctx, job.source, dir, job.index)
			if err == nil {
				results[i] = download{path: path, source: job.source, label: job.label}
			}
		}(i, job)
	}
	wg.Wait()
	for _, found := range results {
		if found.path == "" {
			continue
		}
		if len(cloned.Images)+len(newly) >= 7 {
			// Only retain the files actually selected for the news item.
			_ = os.Remove(found.path)
			continue
		}
		newly = append(newly, found.path)
		sources = append(sources, found.source)
		labels = append(labels, found.label)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	current := a.getItemLocked(id)
	if current == nil {
		for _, p := range newly {
			_ = os.Remove(p)
		}
		return errors.New("Новость удалена")
	}
	if current.State != "new" && current.State != "rejected" {
		for _, p := range newly {
			_ = os.Remove(p)
		}
		current.MediaStatus = "pending"
		return errors.New("Статус изменился")
	}
	if reset && len(newly) == 0 {
		if len(current.Images) >= 3 {
			current.MediaStatus = "ready"
		} else {
			current.MediaStatus = "insufficient"
		}
		current.MediaError = "Новая галерея не найдена, старые изображения сохранены"
		_ = a.persistLocked()
		return fmt.Errorf("Новая галерея не найдена; исходные изображения сохранены")
	}
	if reset {
		current.Awards = nil
		current.AwardsOriginal = ""
		current.AwardsHero = ""
		current.Images = nil
		current.MediaSources = nil
		current.MediaLabels = nil
	}
	available := 7 - len(current.Images)
	if available < 0 {
		available = 0
	}
	if len(newly) > available {
		for _, p := range newly[available:] {
			_ = os.Remove(p)
		}
		newly = newly[:available]
		sources = sources[:available]
		labels = labels[:available]
	}
	for len(current.MediaSources) < len(current.Images) {
		current.MediaSources = append(current.MediaSources, "")
	}
	// Keep existing user-selected order. For a fresh selection, the official
	// hero is always placed first, followed by related screenshots.
	current.Images = append(current.Images, newly...)
	if len(newly) > 0 {
		current.Verified = false
	}
	for len(current.MediaLabels) < len(current.Images)-len(newly) {
		current.MediaLabels = append(current.MediaLabels, "Не классифицировано")
	}
	current.MediaSources = append(current.MediaSources, sources...)
	current.MediaLabels = append(current.MediaLabels, labels...)
	if focus != "" {
		current.MediaFocus = focus
	}
	if len(current.Images) >= 3 {
		current.MediaStatus = "ready"
		current.MediaError = ""
	} else {
		current.MediaStatus = "insufficient"
		if lookupErr != nil {
			current.MediaError = lookupErr.Error()
		} else {
			current.MediaError = fmt.Sprintf("Найдено %d изображений, нужно минимум 3", len(current.Images))
		}
	}
	if err := a.persistLocked(); err != nil {
		return err
	}
	return lookupErr
}

// The most relevant new entries receive media automatically; others remain available
// through the manual "Подобрать изображения" button. Never publish without approval.
func (a *app) prepareMediaBatch() {
	a.mu.Lock()
	queue := []*Item{}
	for _, it := range a.state.Items {
		if it.State == "new" && (it.MediaStatus == "pending" || it.MediaStatus == "") && (it.Source == "Steam News" || it.Source == "Workshop" || it.Source == "Steam Release Radar") {
			cloned := *it
			queue = append(queue, &cloned)
		}
	}
	a.mu.Unlock()
	sort.Slice(queue, func(i, j int) bool {
		if queue[i].Priority != queue[j].Priority {
			return queue[i].Priority > queue[j].Priority
		}
		return queue[i].Time.After(queue[j].Time)
	})
	if len(queue) > 8 {
		queue = queue[:8]
	}
	for _, it := range queue {
		ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
		_ = a.prepareImages(ctx, it.ID)
		cancel()
	}
}
