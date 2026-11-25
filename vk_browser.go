package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func vkBrowserFingerprint(it Item) string {
	h := sha256.New()
	io.WriteString(h, it.Draft)
	io.WriteString(h, "\n"+it.ScheduledAt.UTC().Format(time.RFC3339Nano))
	for _, p := range it.Images {
		io.WriteString(h, "\n"+p)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// VKBrowserHandoff is local preparation ONLY. Nothing is sent to VK.
type VKBrowserHandoff struct {
	OK          bool   `json:"ok"`
	Text        string `json:"text"`
	Folder      string `json:"folder"`
	Images      int    `json:"images"`
	SuggestedAt string `json:"suggested_at,omitempty"`
	Community   string `json:"community"`
}

func (a *app) prepareVKBrowser(id string) (VKBrowserHandoff, error) {
	a.mu.Lock()
	item := a.getItemLocked(id)
	if item == nil {
		a.mu.Unlock()
		return VKBrowserHandoff{}, errors.New("Новость не найдена")
	}
	if item.VKPostID > 0 || item.State == "uncertain" || item.State == "vk_postponed" || item.State == "published" || item.State == "publishing" || item.State == "vk_sending" || item.State == "vk_browser_marked" {
		a.mu.Unlock()
		return VKBrowserHandoff{}, errors.New("У записи уже есть VK ID или неопределённый статус. Проверь VK, чтобы не создать дубль")
	}
	if item.State != "approved" && item.State != "vk_pending" && item.State != "vk_pending_error" && item.State != "scheduled" && item.State != "missed" {
		a.mu.Unlock()
		return VKBrowserHandoff{}, errors.New("Сначала проверь и одобри пост")
	}
	if !item.Verified || len(item.Images) < 3 || len(item.Images) > 7 || !hasGameHashtags(item.Draft, item.Game) {
		a.mu.Unlock()
		return VKBrowserHandoff{}, errors.New("Нужны подтверждённый первоисточник, 3–7 изображений и обязательные хештеги")
	}
	copyItem := *item
	copyItem.Images = append([]string(nil), item.Images...)
	community := a.state.Config.VKCommunityURL
	a.mu.Unlock()

	// Export under the app data directory, not the Windows/Edge browser profile.
	folder := filepath.Join(a.dataDir, "VK_Export", safeID(id), time.Now().Format("20060102-150405.000000000"))
	if err := os.MkdirAll(folder, 0700); err != nil {
		return VKBrowserHandoff{}, err
	}
	complete := false
	defer func() {
		if !complete {
			_ = os.RemoveAll(folder)
		}
	}()
	imagesDir := filepath.Join(a.dataDir, "images")
	for n, img := range copyItem.Images {
		path, err := filepath.Abs(img)
		if err != nil {
			return VKBrowserHandoff{}, err
		}
		rel, err := filepath.Rel(imagesDir, path)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
			return VKBrowserHandoff{}, errors.New("Изображение находится вне папки News Agent")
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".jpg" && ext != ".jpeg" && ext != ".png" {
			return VKBrowserHandoff{}, fmt.Errorf("Неподдерживаемое изображение %d", n+1)
		}
		in, err := os.Open(path)
		if err != nil {
			return VKBrowserHandoff{}, fmt.Errorf("Изображение %d недоступно: %w", n+1, err)
		}
		out, err := os.OpenFile(filepath.Join(folder, fmt.Sprintf("%02d%s", n+1, ext)), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			in.Close()
			return VKBrowserHandoff{}, err
		}
		_, copyErr := io.Copy(out, io.LimitReader(in, 20<<20))
		closeErr := out.Close()
		in.Close()
		if copyErr != nil {
			return VKBrowserHandoff{}, copyErr
		}
		if closeErr != nil {
			return VKBrowserHandoff{}, closeErr
		}
	}
	if err := os.WriteFile(filepath.Join(folder, "POST.txt"), []byte(copyItem.Draft+"\r\n"), 0600); err != nil {
		return VKBrowserHandoff{}, err
	}
	txt := "MALOWAR GAMING · ПЕРЕДАЧА В VK\r\n\r\n1. Открой окно VK из News Agent и войди один раз.\r\n2. Создай новую запись от имени сообщества.\r\n3. В News Agent нажми «Перенести текст и фото (бета)». При неудаче можно вручную вставить текст POST.txt и выбрать фотографии 01–07.\r\n4. Выбери отложенную публикацию и время в редакторе VK.\r\n5. Сохрани запись в VK и только затем отметь её в News Agent.\r\n\r\nВажно: экспорт папки НЕ означает отправку или принятие записи VK.\r\n"
	if !copyItem.ScheduledAt.IsZero() {
		txt += "Предложенное время (Москва): " + copyItem.ScheduledAt.In(moscow).Format("02.01.2006 15:04") + "\r\n"
	}
	if err := os.WriteFile(filepath.Join(folder, "README.txt"), []byte(txt), 0600); err != nil {
		return VKBrowserHandoff{}, err
	}
	a.mu.Lock()
	current := a.getItemLocked(id)
	if current == nil || current.VKPostID > 0 || current.State == "vk_browser_marked" {
		a.mu.Unlock()
		return VKBrowserHandoff{}, errors.New("Статус новости изменился во время экспорта")
	}
	current.VKBrowserExportDir = folder
	current.VKBrowserPreparedAt = time.Now().UTC()
	current.VKBrowserFingerprint = vkBrowserFingerprint(copyItem)
	current.VKBrowserAutoFillAt = time.Time{}
	current.VKBrowserAutoPhotos = false
	if current.State == "vk_pending" || current.State == "vk_pending_error" || current.State == "scheduled" || current.State == "missed" {
		current.State = "approved"
		current.VKQueueError = ""
	}
	a.auditLocked("vk_browser_prepared", id, "Материалы собраны локально; VK не вызывался")
	err := a.persistLocked()
	a.mu.Unlock()
	if err != nil {
		return VKBrowserHandoff{}, err
	}
	complete = true
	result := VKBrowserHandoff{OK: true, Text: copyItem.Draft, Folder: folder, Images: len(copyItem.Images), Community: community}
	if !copyItem.ScheduledAt.IsZero() {
		result.SuggestedAt = copyItem.ScheduledAt.In(moscow).Format("02.01.2006 15:04 МСК")
	}
	return result, nil
}
func (a *app) revealVKBrowserExport(id string) error {
	a.mu.Lock()
	it := a.getItemLocked(id)
	if it == nil {
		a.mu.Unlock()
		return errors.New("Новость не найдена")
	}
	folder := it.VKBrowserExportDir
	a.mu.Unlock()
	base := filepath.Join(a.dataDir, "VK_Export")
	rel, err := filepath.Rel(base, folder)
	if err != nil || folder == "" || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return errors.New("Сначала подготовь пакет публикации")
	}
	if info, err := os.Stat(folder); err != nil || !info.IsDir() {
		return errors.New("Папка экспорта не найдена. Подготовь пакет заново")
	}
	return revealVKFolder(folder)
}
func (a *app) markVKBrowser(id string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	it := a.getItemLocked(id)
	if it == nil {
		return errors.New("Новость не найдена")
	}
	if it.State != "approved" || it.VKBrowserPreparedAt.IsZero() || it.VKBrowserFingerprint != vkBrowserFingerprint(*it) {
		return errors.New("Сначала подготовь одобренную запись")
	}
	it.VKBrowserMarkedAt = time.Now().UTC()
	it.State = "vk_browser_marked"
	a.auditLocked("vk_browser_marked", id, "Пользователь вручную отметил запись в VK. API подтверждения нет")
	return a.persistLocked()
}
func (a *app) unmarkVKBrowser(id string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	it := a.getItemLocked(id)
	if it == nil || it.State != "vk_browser_marked" {
		return errors.New("Запись не отмечена как переданная")
	}
	it.State = "approved"
	it.VKBrowserMarkedAt = time.Time{}
	a.auditLocked("vk_browser_unmark", id, "Отмена ручной отметки; запись VK НЕ изменена")
	return a.persistLocked()
}
