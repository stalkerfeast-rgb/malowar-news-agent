package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// Coordinates are relative to 1280x720, so layouts remain editable at any UI scale.
// Original cover and the last render are separate on disk; publication uses Images[0].
type AwardLayer struct {
	Kind  string  `json:"kind"`
	X     float64 `json:"x"`
	Y     float64 `json:"y"`
	Width float64 `json:"width"`
}
type CoverAwards struct {
	MirrorCover bool         `json:"mirror_cover"`
	MirrorHero  bool         `json:"mirror_hero"`
	HeroX       float64      `json:"hero_x"`
	HeroY       float64      `json:"hero_y"`
	HeroWidth   float64      `json:"hero_width"`
	Layers      []AwardLayer `json:"layers"`
}

func valid01(v float64) bool { return v >= 0 && v <= 1 } // NaN also rejected
func validateCoverAwards(c CoverAwards) error {
	if len(c.Layers) > 3 {
		return errors.New("На одной обложке может быть максимум три награды")
	}
	kinds := map[string]bool{}
	for _, a := range c.Layers {
		if !awardIDs.MatchString(a.Kind) {
			return errors.New("Неверный ID награды")
		}
		if kinds[a.Kind] {
			return errors.New("Одинаковая награда выбрана дважды")
		}
		kinds[a.Kind] = true
		if !valid01(a.X) || !valid01(a.Y) || a.Width < 0.14 || a.Width > 0.85 || a.X+a.Width > 1.00001 || a.Y+(a.Width*219/1433*1280/720) > 1.00001 {
			return errors.New("Награда выходит за пределы обложки")
		}
	}
	if !valid01(c.HeroX) || !valid01(c.HeroY) || !valid01(c.HeroWidth) || c.HeroWidth < 0.05 {
		return errors.New("Некорректные координаты персонажа")
	}
	return nil
}

func imagePNG(body []byte) error {
	if len(body) == 0 || len(body) > 12<<20 {
		return errors.New("PNG должен быть меньше 12 МБ")
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(body))
	if err != nil {
		return errors.New("Нужен корректный PNG")
	}
	if cfg.Width != 1280 || cfg.Height != 720 {
		return fmt.Errorf("Нужна обложка 1280×720, получено %d×%d", cfg.Width, cfg.Height)
	}
	return nil
}
func awardEditable(it *Item) bool {
	return it != nil && (it.State == "new" || it.State == "rejected" || it.State == "approved" || it.State == "missed") && len(it.Images) > 0
}
func (a *app) coverSource(w http.ResponseWriter, r *http.Request, which string) {
	a.mu.Lock()
	it := a.getItemLocked(r.URL.Query().Get("id"))
	path := ""
	if it != nil {
		switch which {
		case "original":
			if it.AwardsOriginal != "" {
				path = it.AwardsOriginal
			} else if len(it.Images) > 0 {
				path = it.Images[0]
			}
		case "hero":
			path = it.AwardsHero
		}
	}
	a.mu.Unlock()
	if path == "" {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, path)
}

func (a *app) saveCoverAwards(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 29<<20)
	if err := r.ParseMultipartForm(29 << 20); err != nil {
		failure(w, 413, "Слишком большой пакет данных")
		return
	}
	id := r.FormValue("id")
	raw := r.FormValue("layout")
	if len(raw) > 16000 {
		failure(w, 400, "Некорректные настройки")
		return
	}
	var layout CoverAwards
	if err := json.Unmarshal([]byte(raw), &layout); err != nil {
		failure(w, 400, "Ошибка настроек: "+err.Error())
		return
	}
	if err := validateCoverAwards(layout); err != nil {
		failure(w, 400, err.Error())
		return
	}
	f, _, err := r.FormFile("cover")
	if err != nil {
		failure(w, 400, "Не передан PNG обложки")
		return
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, (12<<20)+1))
	if err != nil {
		failure(w, 400, "Ошибка чтения PNG")
		return
	}
	if err := imagePNG(b); err != nil {
		failure(w, 400, err.Error())
		return
	}
	// Optional transparent hero layer is stored independently for later editing.
	var hero []byte
	if h, _, e := r.FormFile("hero"); e == nil {
		defer h.Close()
		hero, e = io.ReadAll(io.LimitReader(h, (12<<20)+1))
		if e != nil || len(hero) > 12<<20 {
			failure(w, 400, "Слишком большой слой персонажа")
			return
		}
		cfg, e := png.DecodeConfig(bytes.NewReader(hero))
		if e != nil || cfg.Width < 8 || cfg.Height < 8 || cfg.Width > 4096 || cfg.Height > 4096 {
			failure(w, 400, "Некорректное изображение персонажа")
			return
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	it := a.getItemLocked(id)
	if !awardEditable(it) {
		failure(w, 409, "Обложка доступна только в редактируемой новости с изображениями")
		return
	}
	for _, layer := range layout.Layers {
		badge := a.awardCatalogLocked(layer.Kind)
		if badge == nil || badge.Status != "ready" {
			failure(w, 400, "У награды нет утверждённой графики: "+layer.Kind)
			return
		}
	}
	dir := filepath.Join(a.dataDir, "images", safeID(id))
	if err = os.MkdirAll(dir, 0700); err != nil {
		failure(w, 500, err.Error())
		return
	}
	original := it.AwardsOriginal
	if original == "" {
		old := it.Images[0]
		ext := strings.ToLower(filepath.Ext(old))
		if ext != ".jpg" && ext != ".jpeg" && ext != ".png" {
			failure(w, 400, "Неподдерживаемая исходная обложка")
			return
		}
		original = filepath.Join(dir, "awards-original"+ext)
		if filepath.Clean(old) != filepath.Clean(original) {
			if e := copyCoverFile(old, original); e != nil {
				failure(w, 500, "Не удалось сохранить оригинал: "+e.Error())
				return
			}
		}
	}
	var newHero string
	if len(hero) > 0 {
		f, e := os.CreateTemp(dir, "awards-hero-*.png")
		if e != nil {
			failure(w, 500, e.Error())
			return
		}
		newHero = f.Name()
		_, e = f.Write(hero)
		closeErr := f.Close()
		if e == nil {
			e = closeErr
		}
		if e != nil {
			_ = os.Remove(newHero)
			failure(w, 500, e.Error())
			return
		}
	} else if r.FormValue("clear_hero") == "1" {
		newHero = ""
	} else {
		newHero = it.AwardsHero
	}
	file, e := os.CreateTemp(dir, "awards-cover-*.png")
	if e != nil {
		failure(w, 500, e.Error())
		return
	}
	path := file.Name()
	_, e = file.Write(b)
	closeErr := file.Close()
	if e == nil {
		e = closeErr
	}
	if e != nil {
		_ = os.Remove(path)
		failure(w, 500, e.Error())
		return
	}
	oldImage, oldOriginal, oldHero, oldAwards, oldVerified, oldState, oldLabel := it.Images[0], it.AwardsOriginal, it.AwardsHero, it.Awards, it.Verified, it.State, ""
	for len(it.MediaLabels) < len(it.Images) {
		it.MediaLabels = append(it.MediaLabels, "Не классифицировано")
	}
	oldLabel = it.MediaLabels[0]
	it.Images[0] = path
	it.AwardsOriginal = original
	it.AwardsHero = newHero
	it.Awards = &layout
	it.MediaLabels[0] = "Редакционная обложка · утверждённые награды (оригинал сохранён)"
	it.Verified = false
	if it.State == "approved" || it.State == "missed" {
		it.State = "new"
	}
	a.auditLocked("cover-awards", id, "Наград на обложке: "+fmt.Sprint(len(layout.Layers))+"; одобрение сброшено")
	if e := a.persistLocked(); e != nil {
		it.Images[0] = oldImage
		it.AwardsOriginal = oldOriginal
		it.AwardsHero = oldHero
		it.Awards = oldAwards
		it.Verified = oldVerified
		it.State = oldState
		it.MediaLabels[0] = oldLabel
		_ = os.Remove(path)
		failure(w, 500, e.Error())
		return
	}
	if strings.Contains(filepath.Base(oldImage), "awards-cover-") {
		_ = os.Remove(oldImage)
	}
	jsonResponse(w, 200, map[string]any{"ok": true})
}

func (a *app) resetCoverAwards(w http.ResponseWriter, r *http.Request) {
	var p struct {
		ID string `json:"id"`
	}
	if e := decode(r, &p); e != nil {
		failure(w, 400, "Неверный запрос")
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	it := a.getItemLocked(p.ID)
	if !awardEditable(it) || it.AwardsOriginal == "" {
		failure(w, 409, "Нет исходной обложки для восстановления")
		return
	}
	if _, e := os.Stat(it.AwardsOriginal); e != nil {
		failure(w, 409, "Исходная обложка недоступна")
		return
	}
	it.Images[0] = it.AwardsOriginal
	it.Awards = nil
	it.AwardsOriginal = ""
	it.AwardsHero = ""
	it.Verified = false
	if it.State == "approved" || it.State == "missed" {
		it.State = "new"
	}
	for len(it.MediaLabels) < len(it.Images) {
		it.MediaLabels = append(it.MediaLabels, "")
	}
	it.MediaLabels[0] = "Обложка восстановлена до наложения наград"
	a.auditLocked("cover-awards-reset", p.ID, "Восстановлен исходный баннер")
	if e := a.persistLocked(); e != nil {
		failure(w, 500, e.Error())
		return
	}
	jsonResponse(w, 200, map[string]any{"ok": true})
}
func copyCoverFile(src, dst string) error {
	f, e := os.Open(src)
	if e != nil {
		return e
	}
	defer f.Close()
	tmp, e := os.CreateTemp(filepath.Dir(dst), "cover-original-*.tmp")
	if e != nil {
		return e
	}
	_, e = io.Copy(tmp, io.LimitReader(f, 15<<20))
	if ce := tmp.Close(); e == nil {
		e = ce
	}
	if e != nil {
		os.Remove(tmp.Name())
		return e
	}
	e = os.Rename(tmp.Name(), dst)
	if e != nil {
		os.Remove(tmp.Name())
	}
	return e
}
