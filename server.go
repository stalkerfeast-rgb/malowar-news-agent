package main

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed app/index.html app/mascot.png app/favicon.ico app/awards/*.png app/awards_editor.js
var webFiles embed.FS

// Blob URLs are created from authenticated, locally served media for previews.
// Remote images remain prohibited by img-src.
const pageContentSecurityPolicy = "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'"

func safeID(id string) string { sum := sha256.Sum256([]byte(id)); return hex.EncodeToString(sum[:12]) }
func jsonResponse(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
func failure(w http.ResponseWriter, code int, msg string) {
	jsonResponse(w, code, map[string]any{"error": msg})
}
func decode(r *http.Request, d any) error {
	r.Body = http.MaxBytesReader(nil, r.Body, 2<<20)
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(d)
}
func (a *app) serve() error {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	serverURL := fmt.Sprintf("http://127.0.0.1:%d/", listener.Addr().(*net.TCPAddr).Port)
	stop := make(chan struct{})
	var stopOnce sync.Once
	requestExit := func() { stopOnce.Do(func() { close(stop) }) }
	mux := http.NewServeMux()
	// The logo is bundled with the executable; users never need to fetch remote artwork.
	mux.HandleFunc("/assets/mascot.png", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		data, _ := webFiles.ReadFile("app/mascot.png")
		_, _ = w.Write(data)
	})
	mux.HandleFunc("/assets/awards_editor.js", func(w http.ResponseWriter, r *http.Request) {
		data, err := webFiles.ReadFile("app/awards_editor.js")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(data)
	})
	mux.HandleFunc("/assets/awards/", func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/assets/awards/")
		if !strings.HasSuffix(name, ".png") {
			http.NotFound(w, r)
			return
		}
		data, err := a.awardPNG(strings.TrimSuffix(name, ".png"))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		_, _ = w.Write(data)
	})
	mux.HandleFunc("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/x-icon")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		data, _ := webFiles.ReadFile("app/favicon.ico")
		_, _ = w.Write(data)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		b, _ := webFiles.ReadFile("app/index.html")
		page := strings.ReplaceAll(string(b), "__APP_SECRET__", a.secret)
		page = strings.Replace(page, "<title>Malowar News Agent</title>", "<title>"+appWindowTitle(serverURL)+"</title>", 1)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-MNA-Agent", "v1.3.19")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", pageContentSecurityPolicy)
		_, _ = io.WriteString(w, page)
	})
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		if r.Header.Get("X-MNA-Key") != a.secret {
			failure(w, 403, "Нет доступа")
			return
		}
		switch r.URL.Path {
		case "/api/update-check":
			if r.Method != "GET" {
				failure(w, 405, "Метод не разрешён")
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), 18*time.Second)
			defer cancel()
			info, err := a.updateCheck(ctx)
			if err != nil {
				failure(w, 409, err.Error())
				return
			}
			jsonResponse(w, 200, info)
		case "/api/update-install":
			if r.Method != "POST" {
				failure(w, 405, "Метод не разрешён")
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), 160*time.Second)
			defer cancel()
			if err := a.beginUpdate(ctx); err != nil {
				failure(w, 409, err.Error())
				return
			}
			jsonResponse(w, 202, map[string]bool{"started": true})
			go func() { time.Sleep(700 * time.Millisecond); os.Exit(0) }()
		case "/api/state":
			if r.Method != "GET" {
				failure(w, 405, "Метод не разрешён")
				return
			}
			jsonResponse(w, 200, a.snapshot())
		case "/api/hybrid-approve":
			if r.Method != "POST" {
				failure(w, 405, "Метод не разрешён")
				return
			}
			var p struct {
				AppID    int  `json:"app_id"`
				Approved bool `json:"approved"`
			}
			if err := decode(r, &p); err != nil {
				failure(w, 400, "Неверные данные")
				return
			}
			if err := a.approveHybrid(p.AppID, p.Approved); err != nil {
				failure(w, 409, err.Error())
				return
			}
			jsonResponse(w, 200, map[string]bool{"ok": true})
		case "/api/radar-scan":
			if r.Method != "POST" {
				failure(w, 405, "Метод не разрешён")
				return
			}
			if err := a.radarCheckNow(); err != nil {
				failure(w, 409, err.Error())
				return
			}
			jsonResponse(w, 202, map[string]bool{"started": true})
		case "/api/release-preview":
			if r.Method != "POST" {
				failure(w, 405, "Метод не разрешён")
				return
			}
			var p struct {
				AppID int `json:"app_id"`
			}
			if err := decode(r, &p); err != nil || p.AppID <= 0 {
				failure(w, 400, "Неверный AppID")
				return
			}
			id, err := a.prepareUpcomingDraft(p.AppID, time.Now())
			if err != nil {
				failure(w, 409, err.Error())
				return
			}
			jsonResponse(w, 200, map[string]string{"id": id})
		case "/api/check":
			if r.Method != "POST" {
				failure(w, 405, "Метод не разрешён")
				return
			}
			a.checkNow()
			jsonResponse(w, 202, map[string]bool{"started": true})
		case "/api/settings":
			if r.Method != "POST" {
				failure(w, 405, "Метод не разрешён")
				return
			}
			var c Config
			if err := decode(r, &c); err != nil {
				failure(w, 400, "Неправильные настройки: "+err.Error())
				return
			}
			if err := a.updateConfig(c); err != nil {
				failure(w, 400, err.Error())
				return
			}
			jsonResponse(w, 200, map[string]bool{"ok": true})
		case "/api/local-ai":
			if r.Method == "GET" {
				ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
				defer cancel()
				jsonResponse(w, 200, a.localAIStatus(ctx))
				return
			}
			if r.Method != "POST" {
				failure(w, 405, "Метод не разрешён")
				return
			}
			var p struct {
				Enabled bool `json:"enabled"`
			}
			if decode(r, &p) != nil {
				failure(w, 400, "Неверные данные")
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
			defer cancel()
			message, err := a.setLocalAI(ctx, p.Enabled)
			if err != nil {
				failure(w, 409, err.Error())
				return
			}
			jsonResponse(w, 200, map[string]any{"enabled": p.Enabled, "message": message})
		case "/api/ai-key":
			if r.Method != "POST" {
				failure(w, 405, "Метод не разрешён")
				return
			}
			var p struct {
				Key string `json:"key"`
			}
			if decode(r, &p) != nil || len(p.Key) > 400 {
				failure(w, 400, "Неверные данные")
				return
			}
			a.mu.Lock()
			a.aiKey = strings.TrimSpace(p.Key)
			a.mu.Unlock()
			jsonResponse(w, 200, map[string]bool{"ok": true})
		case "/api/rewrite":
			if r.Method != "POST" {
				failure(w, 405, "Метод не разрешён")
				return
			}
			var p struct {
				ID    string `json:"id"`
				Voice string `json:"voice"`
			}
			if decode(r, &p) != nil {
				failure(w, 400, "Неверные данные")
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), 240*time.Second)
			defer cancel()
			if p.Voice == "" {
				p.Voice = "news"
			}
			if err := a.rewriteItemAs(ctx, p.ID, p.Voice); err != nil {
				failure(w, 409, err.Error())
				return
			}
			jsonResponse(w, 200, map[string]bool{"ok": true})
		case "/api/choose-variant":
			if r.Method != "POST" {
				failure(w, 405, "Метод не разрешён")
				return
			}
			var p struct {
				ID      string `json:"id"`
				Variant string `json:"variant"`
			}
			if err := decode(r, &p); err != nil {
				failure(w, 400, "Неверные данные")
				return
			}
			if err := a.chooseAIVariant(p.ID, p.Variant); err != nil {
				failure(w, 409, err.Error())
				return
			}
			jsonResponse(w, 200, map[string]bool{"ok": true})
		case "/api/retry-comment":
			failure(w, 410, "Комментарии отключены в браузерном режиме VK")
			return
		case "/api/token":
			if r.Method != "POST" {
				failure(w, 405, "Метод не разрешён")
				return
			}
			var p struct {
				Token string `json:"token"`
			}
			if err := decode(r, &p); err != nil {
				failure(w, 400, "Неверные данные")
				return
			}
			if len(p.Token) > 800 {
				failure(w, 400, "Некорректный токен")
				return
			}
			if err := a.setToken(p.Token); err != nil {
				failure(w, 500, err.Error())
				return
			}
			if strings.TrimSpace(p.Token) != "" {
				go a.processVKQueue(time.Now().UTC())
			}
			jsonResponse(w, 200, map[string]bool{"ok": true})
		case "/api/vk-check":
			if r.Method != "POST" {
				failure(w, 405, "Метод не разрешён")
				return
			}
			a.mu.Lock()
			token, group := a.token, a.state.Config.GroupID
			a.mu.Unlock()
			if token == "" {
				failure(w, 409, "VK не подключён. Ранее сохранённый токен не найден")
				return
			}
			if !validGroup(group) {
				failure(w, 409, "Сначала сохрани числовой ID сообщества VK")
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
			defer cancel()
			// No wall.post and no photo upload: only a non-publishing permission probe.
			_, err := vkCall(ctx, token, "photos.getWallUploadServer", url.Values{"group_id": {group}})
			if err != nil {
				failure(w, 409, "Проверка VK отклонена: "+err.Error()+". Сохранённый токен не удалён")
				return
			}
			jsonResponse(w, 200, map[string]any{"ok": true, "message": "VK подтвердил доступ к загрузке фото; публикация проверяется отдельно"})
		case "/api/editorial-tag":
			if r.Method != "POST" {
				failure(w, 405, "Метод не разрешён")
				return
			}
			var p struct {
				ID         string `json:"id"`
				Bucket     string `json:"bucket"`
				ParentGame string `json:"parent_game"`
			}
			if err := decode(r, &p); err != nil {
				failure(w, 400, "Неверные данные")
				return
			}
			if err := a.setEditorialTag(p.ID, p.Bucket, p.ParentGame); err != nil {
				failure(w, 400, err.Error())
				return
			}
			jsonResponse(w, 200, map[string]bool{"ok": true})
		case "/api/item":
			if r.Method != "POST" {
				failure(w, 405, "Метод не разрешён")
				return
			}
			var p struct {
				ID       string `json:"id"`
				Draft    string `json:"draft"`
				Comment  string `json:"comment"`
				State    string `json:"state"`
				Verified bool   `json:"verified"`
			}
			if err := decode(r, &p); err != nil {
				failure(w, 400, "Неверные данные")
				return
			}
			if err := a.updateItem(p.ID, p.Draft, p.Comment, p.State, p.Verified); err != nil {
				failure(w, 400, err.Error())
				return
			}
			if p.State == "approved" {
				go a.processVKQueue(time.Now().UTC())
			}
			jsonResponse(w, 200, map[string]bool{"ok": true})
		case "/api/award-add":
			if r.Method != "POST" {
				failure(w, 405, "Метод не разрешён")
				return
			}
			var x struct {
				Name     string `json:"name"`
				Category string `json:"category"`
			}
			if decode(r, &x) != nil {
				failure(w, 400, "Неверные данные")
				return
			}
			if err := a.addAwardCandidate(x.Name, x.Category); err != nil {
				failure(w, 400, err.Error())
				return
			}
			jsonResponse(w, 200, map[string]bool{"ok": true})
		case "/api/award-upload":
			if r.Method != "POST" {
				failure(w, 405, "Метод не разрешён")
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, 9<<20)
			if err := r.ParseMultipartForm(9 << 20); err != nil {
				failure(w, 400, "PNG слишком большой")
				return
			}
			f, _, err := r.FormFile("image")
			if err != nil {
				failure(w, 400, "Загрузи PNG")
				return
			}
			defer f.Close()
			if err = a.uploadAwardPNG(r.FormValue("id"), f); err != nil {
				failure(w, 400, err.Error())
				return
			}
			jsonResponse(w, 200, map[string]bool{"ok": true})
		case "/api/vk-queue":
			failure(w, 410, "Отправка через VK API отключена. Используй браузер VK")
			return
		case "/api/vk-status-refresh":
			failure(w, 410, "Проверка VK API отключена в браузерном режиме")
			return
		case "/api/cover-original":
			if r.Method != "GET" {
				failure(w, 405, "Метод не разрешён")
				return
			}
			a.coverSource(w, r, "original")
		case "/api/cover-hero":
			if r.Method != "GET" {
				failure(w, 405, "Метод не разрешён")
				return
			}
			a.coverSource(w, r, "hero")
		case "/api/cover-awards":
			if r.Method != "POST" {
				failure(w, 405, "Метод не разрешён")
				return
			}
			a.saveCoverAwards(w, r)
		case "/api/cover-awards-reset":
			if r.Method != "POST" {
				failure(w, 405, "Метод не разрешён")
				return
			}
			a.resetCoverAwards(w, r)
		case "/api/auto-images":
			if r.Method != "POST" {
				failure(w, 405, "Метод не разрешён")
				return
			}
			var p struct {
				ID    string `json:"id"`
				Reset bool   `json:"reset"`
			}
			if decode(r, &p) != nil {
				failure(w, 400, "Неверные данные")
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
			defer cancel()
			var err error
			if p.Reset {
				err = a.rebuildThematicImages(ctx, p.ID)
			} else {
				err = a.prepareImages(ctx, p.ID)
			}
			if err != nil {
				failure(w, 409, "Автоподбор: "+err.Error())
				return
			}
			jsonResponse(w, 200, map[string]bool{"ok": true})
		case "/api/upload":
			if r.Method != "POST" {
				failure(w, 405, "Метод не разрешён")
				return
			}
			a.handleUpload(w, r)
		case "/api/external-image":
			if r.Method != "POST" {
				failure(w, 405, "Метод не разрешён")
				return
			}
			var p struct {
				ID   string `json:"id"`
				URL  string `json:"url"`
				Kind string `json:"kind"`
			}
			if decode(r, &p) != nil || p.ID == "" || len(p.URL) > 2048 {
				failure(w, 400, "Проверь ссылку и карточку")
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
			defer cancel()
			if err := a.attachExternalMedia(ctx, p.ID, p.URL, p.Kind); err != nil {
				failure(w, 409, err.Error())
				return
			}
			jsonResponse(w, 200, map[string]bool{"ok": true})
		case "/api/hero-image":
			if r.Method != "POST" {
				failure(w, 405, "Метод не разрешён")
				return
			}
			var p struct {
				ID string `json:"id"`
			}
			if decode(r, &p) != nil {
				failure(w, 400, "Неверные данные")
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), 65*time.Second)
			defer cancel()
			if err := a.promoteOfficialHero(ctx, p.ID); err != nil {
				failure(w, 409, err.Error())
				return
			}
			jsonResponse(w, 200, map[string]bool{"ok": true})
		case "/api/image-order":
			if r.Method != "POST" {
				failure(w, 405, "Метод не разрешён")
				return
			}
			var p struct {
				ID   string `json:"id"`
				From int    `json:"from"`
				To   int    `json:"to"`
			}
			if err := decode(r, &p); err != nil {
				failure(w, 400, "Неверный запрос")
				return
			}
			if err := a.moveImage(p.ID, p.From, p.To); err != nil {
				failure(w, 409, err.Error())
				return
			}
			jsonResponse(w, 200, map[string]bool{"ok": true})
		case "/api/remove-image":
			if r.Method != "POST" {
				failure(w, 405, "Метод не разрешён")
				return
			}
			var p struct {
				ID    string `json:"id"`
				Index int    `json:"index"`
			}
			if err := decode(r, &p); err != nil {
				failure(w, 400, "Неверный запрос")
				return
			}
			a.mu.Lock()
			it := a.getItemLocked(p.ID)
			if it == nil || it.State == "published" || it.State == "publishing" || it.State == "uncertain" || it.State == "scheduled" || p.Index < 0 || p.Index >= len(it.Images) {
				a.mu.Unlock()
				failure(w, 400, "Нельзя удалить изображение")
				return
			}
			name := it.Images[p.Index]
			if p.Index == 0 {
				it.Awards = nil
				it.AwardsOriginal = ""
				it.AwardsHero = ""
			}
			if p.Index < len(it.MediaSources) {
				it.MediaSources = append(it.MediaSources[:p.Index], it.MediaSources[p.Index+1:]...)
			}
			if p.Index < len(it.MediaLabels) {
				it.MediaLabels = append(it.MediaLabels[:p.Index], it.MediaLabels[p.Index+1:]...)
			}
			it.Verified = false
			it.MediaStatus = "pending"
			it.Images = append(it.Images[:p.Index], it.Images[p.Index+1:]...)
			_ = a.persistLocked()
			a.mu.Unlock()
			_ = os.Remove(name)
			jsonResponse(w, 200, map[string]bool{"ok": true})
		case "/api/media":
			if r.Method != "GET" {
				failure(w, 405, "Метод не разрешён")
				return
			}
			id := r.URL.Query().Get("id")
			index, _ := strconv.Atoi(r.URL.Query().Get("index"))
			a.mu.Lock()
			it := a.getItemLocked(id)
			name := ""
			if it != nil && index >= 0 && index < len(it.Images) {
				name = it.Images[index]
			}
			a.mu.Unlock()
			if name == "" {
				http.NotFound(w, r)
				return
			}
			// Files in the media directory are validated on upload.
			contentType := mime.TypeByExtension(filepath.Ext(name))
			if contentType == "" {
				contentType = "image/jpeg"
			}
			w.Header().Set("Content-Type", contentType)
			http.ServeFile(w, r, name)
		case "/api/publish":
			failure(w, 410, "Прямая отправка VK API отключена. Используй браузер VK")
			return
		case "/api/vk-browser":
			if r.Method != "POST" {
				failure(w, 405, "Метод не разрешён")
				return
			}
			var req struct {
				ID     string `json:"id"`
				Action string `json:"action"`
			}
			if decode(r, &req) != nil {
				failure(w, 400, "Неверный запрос")
				return
			}
			switch req.Action {
			case "prepare":
				result, err := a.prepareVKBrowser(req.ID)
				if err != nil {
					failure(w, 409, err.Error())
					return
				}
				jsonResponse(w, 200, result)
			case "start":
				result, err := a.startVKPost(req.ID)
				if err != nil {
					failure(w, 409, err.Error())
					return
				}
				jsonResponse(w, 200, result)
			case "fill":
				result, err := a.autoFillVK(req.ID)
				if err != nil {
					failure(w, 409, err.Error())
					return
				}
				jsonResponse(w, 200, result)
			case "open":
				if err := openVKBrowserWindow(a.vkCommunityURL()); err != nil {
					failure(w, 409, err.Error())
					return
				}
				jsonResponse(w, 200, map[string]any{"ok": true, "url": a.vkCommunityURL()})
			case "folder":
				if err := a.revealVKBrowserExport(req.ID); err != nil {
					failure(w, 409, err.Error())
					return
				}
				jsonResponse(w, 200, map[string]bool{"ok": true})
			case "mark":
				if err := a.markVKBrowser(req.ID); err != nil {
					failure(w, 409, err.Error())
					return
				}
				jsonResponse(w, 200, map[string]bool{"ok": true})
			case "unmark":
				if err := a.unmarkVKBrowser(req.ID); err != nil {
					failure(w, 409, err.Error())
					return
				}
				jsonResponse(w, 200, map[string]bool{"ok": true})
			default:
				failure(w, 400, "Неизвестное действие")
			}
			return
		case "/api/schedule":
			if r.Method != "POST" {
				failure(w, 405, "Метод не разрешён")
				return
			}
			var p struct {
				ID   string `json:"id"`
				Mode string `json:"mode"`
				At   string `json:"at"`
			}
			if decode(r, &p) != nil {
				failure(w, 400, "Неверный запрос")
				return
			}
			date, err := a.setSchedule(p.ID, p.Mode, p.At)
			if err != nil {
				failure(w, 409, err.Error())
				return
			}
			jsonResponse(w, 200, map[string]any{"ok": true, "scheduled_at": date})
			if p.Mode != "cancel" {
				go a.processVKQueue(time.Now().UTC())
			}
		case "/api/vk-metrics":
			if r.Method != "POST" {
				failure(w, 405, "Метод не разрешён")
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), 35*time.Second)
			defer cancel()
			count, err := a.refreshVKMetrics(ctx)
			if err != nil {
				failure(w, 409, fmt.Sprintf("Обновлено %d: %v", count, err))
				return
			}
			jsonResponse(w, 200, map[string]any{"ok": true, "updated": count})
		case "/api/analytics":
			if r.Method != "GET" {
				failure(w, 405, "Метод не разрешён")
				return
			}
			jsonResponse(w, 200, a.analytics())
		case "/api/exit":
			if r.Method != "POST" {
				failure(w, 405, "Метод не разрешён")
				return
			}
			jsonResponse(w, 200, map[string]bool{"ok": true})
			requestExit()
		default:
			failure(w, 404, "Не найдено")
		}
	})
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	log.Printf("Malowar News Agent is running locally at %s", serverURL)
	active := filepath.Join(a.dataDir, "active.json")
	defer os.Remove(active)
	if bb, err := json.Marshal(map[string]string{"url": serverURL}); err == nil {
		_ = os.WriteFile(active, bb, 0600)
	}
	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			log.Print(err)
		}
	}()
	closeTray := startAppTray(serverURL, requestExit)
	defer closeTray()
	openAppWindow(serverURL)
	a.mu.Lock()
	auto := a.state.Config.AutoMonitor
	a.mu.Unlock()
	if auto {
		a.checkNow()
		_ = a.radarCheckNow()
	}
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			err := server.Shutdown(ctx)
			cancel()
			if err != nil {
				_ = server.Close()
			}
			return nil
		case <-ticker.C:
		}
		a.mu.Lock()
		should := a.state.Config.AutoMonitor && (!a.checking) && time.Since(a.state.LastCheck) >= time.Duration(a.state.Config.Interval)*time.Minute
		a.mu.Unlock()
		if should {
			a.checkNow()
		}
		a.mu.Lock()
		radarInterval := 2 * time.Hour
		for _, release := range a.state.Radar {
			if !release.ExactDate {
				continue
			}
			days, err := daysUntilMSK(release.Date, time.Now())
			if err == nil && days >= -1 && days <= 1 {
				radarInterval = 10 * time.Minute
				break
			}
		}
		radarDue := a.state.Config.AutoMonitor && !a.radarBusy && time.Since(a.state.RadarChecked) > radarInterval
		a.mu.Unlock()
		if radarDue {
			_ = a.radarCheckNow()
		}
		a.processSchedule(time.Now().UTC())
		a.processVKQueue(time.Now().UTC())
	}
}

func appWindowTitle(localURL string) string {
	return "Malowar News Agent · " + strings.TrimSuffix(strings.TrimPrefix(localURL, "http://127.0.0.1:"), "/")
}
func (a *app) handleUpload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 85<<20)
	if err := r.ParseMultipartForm(90 << 20); err != nil {
		failure(w, 413, "Изображения слишком большие")
		return
	}
	id := r.FormValue("id")
	a.mu.Lock()
	it := a.getItemLocked(id)
	good := it != nil && it.State != "published" && it.State != "publishing" && it.State != "uncertain" && it.State != "scheduled"
	existing := 0
	if it != nil {
		existing = len(it.Images)
	}
	a.mu.Unlock()
	if !good {
		failure(w, 400, "Новость недоступна для редактирования")
		return
	}
	files := r.MultipartForm.File["images"]
	if len(files) == 0 || len(files)+existing > 7 {
		failure(w, 400, "Разрешено от 1 до 7 изображений, включая уже добавленные")
		return
	}
	dir := filepath.Join(a.dataDir, "images", safeID(id))
	if err := os.MkdirAll(dir, 0700); err != nil {
		failure(w, 500, err.Error())
		return
	}
	added := []string{}
	defer func() {
		if len(added) == 0 {
			return
		}
	}()
	for _, fh := range files {
		if fh.Size > 12<<20 {
			for _, p := range added {
				_ = os.Remove(p)
			}
			failure(w, 413, "Каждый файл должен быть не больше 12 МБ")
			return
		}
		f, err := fh.Open()
		if err != nil {
			failure(w, 500, "Не удалось прочитать файл")
			return
		}
		head := make([]byte, 512)
		n, _ := f.Read(head)
		_ = f.Close()
		typ := http.DetectContentType(head[:n])
		ext := ""
		switch typ {
		case "image/jpeg":
			ext = ".jpg"
		case "image/png":
			ext = ".png"
		default:
			for _, p := range added {
				_ = os.Remove(p)
			}
			failure(w, 400, "Только JPG или PNG")
			return
		}
		src, err := fh.Open()
		if err != nil {
			failure(w, 400, "Ошибка файла")
			return
		}
		tmp, err := os.CreateTemp(dir, "img-*"+ext)
		if err != nil {
			_ = src.Close()
			failure(w, 500, err.Error())
			return
		}
		_, err = io.Copy(tmp, io.LimitReader(src, 12<<20))
		_ = src.Close()
		_ = tmp.Close()
		if err != nil {
			_ = os.Remove(tmp.Name())
			failure(w, 500, "Ошибка копирования изображения")
			return
		}
		added = append(added, tmp.Name())
	}
	a.mu.Lock()
	it = a.getItemLocked(id)
	if it == nil || len(it.Images)+len(added) > 7 {
		a.mu.Unlock()
		for _, p := range added {
			_ = os.Remove(p)
		}
		failure(w, 409, "Список изменился")
		return
	}
	for len(it.MediaSources) < len(it.Images) {
		it.MediaSources = append(it.MediaSources, "")
	}
	for len(it.MediaLabels) < len(it.Images) {
		it.MediaLabels = append(it.MediaLabels, "Не классифицировано")
	}
	it.Images = append(it.Images, added...)
	for range added {
		it.MediaSources = append(it.MediaSources, "")
		it.MediaLabels = append(it.MediaLabels, "Загружено вручную")
	}
	it.Verified = false
	if len(it.Images) >= 3 {
		it.MediaStatus = "ready"
	}
	err := a.persistLocked()
	a.mu.Unlock()
	if err != nil {
		failure(w, 500, err.Error())
		return
	}
	jsonResponse(w, 200, map[string]bool{"ok": true})
}
func dataFolder() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	base := filepath.Dir(exe)
	if _, err := os.Stat(filepath.Join(base, ".mna-install.json")); err == nil {
		return filepath.Join(base, "Data"), nil
	}
	if custom := os.Getenv("MALOWAR_NEWS_DATA"); custom != "" {
		return custom, nil
	}
	root := os.Getenv("LOCALAPPDATA")
	if root == "" {
		home, e := os.UserHomeDir()
		if e != nil {
			return "", e
		}
		root = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(root, "MalowarNewsAgent", "Data"), nil
}
func openExisting(dir string) bool {
	b, err := os.ReadFile(filepath.Join(dir, "active.json"))
	if err != nil {
		return false
	}
	var stored struct {
		URL string `json:"url"`
	}
	if json.Unmarshal(b, &stored) != nil {
		return false
	}
	if !strings.HasPrefix(stored.URL, "http://127.0.0.1:") {
		return false
	}
	testClient := http.Client{Timeout: 1200 * time.Millisecond}
	resp, err := testClient.Get(stored.URL)
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	if !strings.HasPrefix(resp.Header.Get("X-MNA-Agent"), "v1.") {
		return false
	}
	openAppWindow(stored.URL)
	return true
}
func main() {
	dir, err := dataFolder()
	if err != nil {
		log.Fatal(err)
	}
	if openExisting(dir) {
		return
	}
	a, err := newApp(dir)
	if err != nil {
		log.Fatal(err)
	}
	a.enableVKCredentialVault() // Restore encrypted VK token for this Windows account.
	// Restart the local Ollama daemon on launch only when the user already enabled it.
	go a.startLocalAIWithSavedConsent()
	if err := a.serve(); err != nil {
		log.Fatal(err)
	}
}
