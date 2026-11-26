package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// VKAPIError means VK returned a definitive API error, not an ambiguous network timeout.
type VKAPIError struct {
	Code int
	Msg  string
}

func (e *VKAPIError) Error() string { return fmt.Sprintf("VK API %d: %s", e.Code, e.Msg) }

func vkCall(ctx context.Context, token, method string, args url.Values) (json.RawMessage, error) {
	if args == nil {
		args = url.Values{}
	}
	args.Set("access_token", token)
	args.Set("v", "5.199")
	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.vk.com/method/"+method, strings.NewReader(args.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("VK HTTP %d", res.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	var result struct {
		Response json.RawMessage `json:"response"`
		Error    *struct {
			Code int    `json:"error_code"`
			Msg  string `json:"error_msg"`
		} `json:"error"`
	}
	if err = json.Unmarshal(b, &result); err != nil {
		return nil, err
	}
	if result.Error != nil {
		return nil, &VKAPIError{Code: result.Error.Code, Msg: result.Error.Msg}
	}
	if len(result.Response) == 0 {
		return nil, errors.New("VK вернул пустой ответ")
	}
	return result.Response, nil
}
func vkUpload(ctx context.Context, token, group, path string) (string, error) {
	resp, err := vkCall(ctx, token, "photos.getWallUploadServer", url.Values{"group_id": {group}})
	if err != nil {
		return "", err
	}
	var info struct {
		UploadURL string `json:"upload_url"`
	}
	if err = json.Unmarshal(resp, &info); err != nil || info.UploadURL == "" {
		return "", errors.New("VK не выдал адрес для фото")
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	w, err := writer.CreateFormFile("photo", filepath.Base(path))
	if err != nil {
		return "", err
	}
	if _, err = io.Copy(w, io.LimitReader(f, 18<<20)); err != nil {
		return "", err
	}
	if err = writer.Close(); err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", info.UploadURL, &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	res, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return "", fmt.Errorf("Загрузка изображения: HTTP %d", res.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if err != nil {
		return "", err
	}
	var uploaded struct {
		Photo  json.RawMessage `json:"photo"`
		Server json.Number     `json:"server"`
		Hash   string          `json:"hash"`
		Error  string          `json:"error"`
	}
	if err = json.Unmarshal(data, &uploaded); err != nil {
		return "", err
	}
	if len(uploaded.Photo) == 0 || uploaded.Hash == "" {
		return "", errors.New("VK не подтвердил загрузку изображения")
	}
	// VK upload servers commonly return a JSON string in photo; preserve exact representation if possible.
	photo := strings.TrimSpace(string(uploaded.Photo))
	if strings.HasPrefix(photo, "\"") {
		var s string
		if json.Unmarshal(uploaded.Photo, &s) == nil {
			photo = s
		}
	}
	saved, err := vkCall(ctx, token, "photos.saveWallPhoto", url.Values{"group_id": {group}, "photo": {photo}, "server": {uploaded.Server.String()}, "hash": {uploaded.Hash}})
	if err != nil {
		return "", err
	}
	var photos []struct {
		ID      int64 `json:"id"`
		OwnerID int64 `json:"owner_id"`
	}
	if err = json.Unmarshal(saved, &photos); err != nil || len(photos) == 0 {
		return "", errors.New("VK не сохранил изображение")
	}
	return fmt.Sprintf("photo%d_%d", photos[0].OwnerID, photos[0].ID), nil
}

// sendSourceComment posts source links only after the wall entry has a confirmed ID.
// On any transport failure we never re-send automatically: the comment may exist.
func sendSourceComment(ctx context.Context, token, group string, postID int64, message, articleID string) (int64, error) {
	if strings.TrimSpace(message) == "" {
		return 0, errors.New("Нет текста для первого комментария")
	}
	args := url.Values{
		"owner_id":   {"-" + group},
		"post_id":    {strconv.FormatInt(postID, 10)},
		"from_group": {group},
		"message":    {message},
		"guid":       {"mna-source-" + safeID(articleID)},
	}
	response, err := vkCall(ctx, token, "wall.createComment", args)
	if err != nil {
		return 0, err
	}
	var result struct {
		CommentID int64 `json:"comment_id"`
		ID        int64 `json:"id"`
	}
	if err := json.Unmarshal(response, &result); err != nil {
		return 0, err
	}
	if result.CommentID > 0 {
		return result.CommentID, nil
	}
	if result.ID > 0 {
		return result.ID, nil
	}
	return 0, errors.New("VK не подтвердил ID комментария")
}
func (a *app) publish(id string) {
	a.mu.Lock()
	if a.posting {
		a.mu.Unlock()
		return
	}
	it := a.getItemLocked(id)
	if it == nil || (it.State != "approved" && it.State != "scheduled") || !it.Verified || len(it.Images) < 3 || len(it.Images) > 7 || a.token == "" || !validGroup(a.state.Config.GroupID) || strings.TrimSpace(it.Comment) == "" {
		a.mu.Unlock()
		return
	}
	a.posting = true
	it.State = "publishing"
	it.Error = ""
	a.auditLocked("publish_attempt", it.ID, "Начата отправка в VK")
	_ = a.persistLocked()
	copyIt := *it
	copyIt.Images = append([]string(nil), it.Images...)
	token, group := a.token, a.state.Config.GroupID
	a.mu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		var attachments []string
		var err error
		for _, img := range copyIt.Images {
			var att string
			att, err = vkUpload(ctx, token, group, img)
			if err != nil {
				break
			}
			attachments = append(attachments, att)
			time.Sleep(450 * time.Millisecond)
		}
		postAttempted := false
		var postID int64
		if err == nil {
			cleanDraft, _ := extractLinks(copyIt.Draft)
			args := url.Values{"owner_id": {"-" + group}, "from_group": {"1"}, "message": {cleanDraft}, "attachments": {strings.Join(attachments, ",")}, "guid": {"malowar-" + safeID(copyIt.ID)}}
			postAttempted = true
			var response json.RawMessage
			response, err = vkCall(ctx, token, "wall.post", args)
			if err == nil {
				var r struct {
					PostID int64 `json:"post_id"`
				}
				if json.Unmarshal(response, &r) == nil && r.PostID > 0 {
					postID = r.PostID
				} else {
					err = errors.New("VK не подтвердил ID публикации")
				}
			}
		}
		if postID <= 0 {
			a.mu.Lock()
			record := a.getItemLocked(id)
			if record != nil {
				if postAttempted {
					record.State = "uncertain"
					record.Error = "Проверь стену VK перед повтором: " + err.Error()
				} else {
					record.State = "approved"
					record.Error = err.Error()
				}
			}
			a.posting = false
			_ = a.persistLocked()
			a.mu.Unlock()
			return
		}
		// A successful wall.post is persisted BEFORE posting the first comment.
		// This prevents duplicate wall posts if the next request fails.
		a.mu.Lock()
		record := a.getItemLocked(id)
		if record != nil {
			record.State = "published"
			record.PublishedAt = time.Now().UTC()
			a.auditLocked("published", record.ID, fmt.Sprintf("VK post ID %d", postID))
			record.VKPostID = postID
			record.CommentState = "sending"
			record.Error = ""
			_ = a.persistLocked()
		}
		a.mu.Unlock()
		commentID, commentErr := sendSourceComment(ctx, token, group, postID, copyIt.Comment, id)
		a.mu.Lock()
		record = a.getItemLocked(id)
		if record != nil {
			if commentErr != nil {
				record.CommentState = "check"
				record.Error = "Пост опубликован, но статус комментария неизвестен. Проверь VK перед ручной повторной отправкой: " + commentErr.Error()
			} else {
				record.CommentState = "sent"
				record.CommentID = commentID
				record.Error = ""
			}
		}
		a.posting = false
		_ = a.persistLocked()
		a.mu.Unlock()
	}()
}

// Manual retry is blocked for unknown comment status to prevent accidental duplicates.
// After verifying VK, user can post the comment manually; the agent never guesses.
func (a *app) retryComment(id string) error {
	a.mu.Lock()
	it := a.getItemLocked(id)
	if it == nil || it.State != "published" || it.VKPostID <= 0 || it.CommentID > 0 || it.CommentState != "error" || a.token == "" || a.posting || !validGroup(a.state.Config.GroupID) {
		a.mu.Unlock()
		return errors.New("Повтор недоступен. Проверь комментарии в VK и размести ссылки вручную, если комментария нет")
	}
	a.posting = true
	it.CommentState = "sending"
	_ = a.persistLocked()
	token, group, post, comment := a.token, a.state.Config.GroupID, it.VKPostID, it.Comment
	a.mu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cid, err := sendSourceComment(ctx, token, group, post, comment, id)
		a.mu.Lock()
		defer a.mu.Unlock()
		if i := a.getItemLocked(id); i != nil {
			if err == nil {
				i.CommentID = cid
				i.CommentState = "sent"
				i.Error = ""
			} else {
				i.CommentState = "check"
				i.Error = "Проверь комментарии в VK: " + err.Error()
			}
		}
		a.posting = false
		_ = a.persistLocked()
	}()
	return nil
}
func (a *app) autoPublishApproved() { a.processSchedule(time.Now().UTC()) }

// enableVKCredentialVault is called only in the actual application entrypoint.
// Tests constructing newApp(t.TempDir()) never open the real user's vault.
func (a *app) enableVKCredentialVault() {
	vault, err := vkVaultPath()
	a.mu.Lock()
	if err != nil {
		a.vkVaultErr = "Не удалось открыть защищённое хранилище Windows: " + err.Error()
		a.mu.Unlock()
		return
	}
	a.vkVault = vault
	a.mu.Unlock()
	token, err := readVKCredential(vault)
	a.mu.Lock()
	defer a.mu.Unlock()
	if err != nil {
		a.vkVaultErr = "Сохранённый ключ VK недоступен. Подключи VK повторно или проверь профиль Windows."
		return
	}
	a.token = token
	a.vkVaultErr = ""
}

// setToken only changes memory after Windows confirms encrypted persistence.
// An empty value removes the saved token as well as disconnecting the session.
func (a *app) setToken(s string) error {
	s = strings.TrimSpace(s)
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.vkVault == "" && a.vkVaultErr != "" {
		return errors.New(a.vkVaultErr)
	}
	if a.vkVault != "" {
		if err := writeVKCredential(a.vkVault, s); err != nil {
			a.vkVaultErr = "Не удалось сохранить токен в защищённом хранилище Windows"
			return errors.New(a.vkVaultErr)
		}
	}
	a.token = s
	a.vkVaultErr = ""
	return nil
}

func validGroup(s string) bool {
	if s == "" {
		return false
	}
	if strings.Trim(s, "0123456789") != "" {
		return false
	}
	n, e := strconv.ParseInt(s, 10, 64)
	return e == nil && n > 0
}
