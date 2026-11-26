package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Queue items are created ONLY on a new explicit approval. Existing approved
// drafts remain local on migration. Posting to VK is never silently replayed
// after an ambiguous network failure.
func (a *app) processVKQueue(now time.Time) {
	a.mu.Lock()
	if a.state.Config.VKBrowserMode || a.posting || a.token == "" || !validGroup(a.state.Config.GroupID) {
		a.mu.Unlock()
		return
	}
	var pending *Item
	var comment *Item
	for _, it := range a.state.Items {
		if it.State == "vk_pending" && pending == nil {
			pending = it
		}
		if it.State == "vk_postponed" && it.VKPostID > 0 && !now.Before(it.ScheduledAt.Add(2*time.Minute)) && comment == nil {
			comment = it
		}
	}
	if pending != nil {
		if !pending.Verified || len(pending.Images) < 3 || len(pending.Images) > 7 || !hasGameHashtags(pending.Draft, pending.Game) || strings.TrimSpace(pending.Comment) == "" {
			pending.State = "vk_pending_error"
			pending.VKQueueError = "Проверь 3–7 изображений, текст, обязательный хэштег и ссылки"
			_ = a.persistLocked()
			a.mu.Unlock()
			return
		}
		// VK must receive a future publish_date even if the PC stayed offline.
		if !pending.ScheduledAt.After(now.Add(10 * time.Minute)) {
			slot, err := a.nextSlotLocked(now.Add(10*time.Minute), pending.ID)
			if err != nil {
				pending.State = "vk_pending_error"
				pending.VKQueueError = err.Error()
				_ = a.persistLocked()
				a.mu.Unlock()
				return
			}
			pending.ScheduledAt = slot
		}
		pending.State = "vk_sending"
		pending.VKQueueError = ""
		a.auditLocked("vk_schedule_attempt", pending.ID, "Создание отложенной записи VK")
		_ = a.persistLocked()
		copyIt := *pending
		copyIt.Images = append([]string(nil), pending.Images...)
		token, group := a.token, a.state.Config.GroupID
		a.posting = true
		a.mu.Unlock()
		go a.sendPostponed(copyIt, token, group)
		return
	}
	if comment != nil {
		id, postID := comment.ID, comment.VKPostID
		token, group := a.token, a.state.Config.GroupID
		a.posting = true
		a.mu.Unlock()
		go a.verifyPublishedPost(id, postID, token, group)
		return
	}
	a.mu.Unlock()
}
func (a *app) sendPostponed(it Item, token, group string) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	attachments := []string{}
	var err error
	for _, img := range it.Images {
		att, upErr := vkUpload(ctx, token, group, img)
		if upErr != nil {
			err = upErr
			break
		}
		attachments = append(attachments, att)
	}
	attempted := false
	var postID int64
	if err == nil {
		cleanDraft, _ := extractLinks(it.Draft)
		args := url.Values{
			"owner_id": {"-" + group}, "from_group": {"1"}, "message": {cleanDraft},
			"attachments":  {strings.Join(attachments, ",")},
			"publish_date": {strconv.FormatInt(it.ScheduledAt.Unix(), 10)},
			"guid":         {"malowar-queue-" + safeID(it.ID)},
		}
		attempted = true
		var resp json.RawMessage
		resp, err = vkCall(ctx, token, "wall.post", args)
		if err == nil {
			var posted struct {
				PostID int64 `json:"post_id"`
			}
			if json.Unmarshal(resp, &posted) == nil && posted.PostID > 0 {
				postID = posted.PostID
			}
			if postID <= 0 {
				err = errors.New("VK не подтвердил ID отложенной записи")
			}
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	record := a.getItemLocked(it.ID)
	if record != nil {
		if postID > 0 {
			record.VKPostID = postID
			record.State = "vk_postponed"
			record.CommentState = "waiting"
			record.VKQueueError = ""
			record.Error = ""
			a.auditLocked("vk_postponed", it.ID, fmt.Sprintf("VK post ID %d, публикация %s МСК", postID, it.ScheduledAt.In(moscow).Format("02.01 15:04")))
		} else if attempted {
			var apiErr *VKAPIError
			if errors.As(err, &apiErr) {
				// An explicit VK API rejection means no record was accepted.
				record.State = "vk_pending_error"
				record.VKQueueError = "VK отказал в создании отложенного поста: " + apiErr.Error()
				record.Error = ""
			} else {
				// After a timeout or an invalid response we cannot safely retry.
				record.State = "uncertain"
				record.Error = "VK мог создать запись, но ответ не подтверждён. Проверь отложенные VK вручную перед повтором: " + err.Error()
			}
		} else {
			record.State = "vk_pending_error"
			record.VKQueueError = "Не удалось загрузить фото: " + err.Error()
		}
	}
	a.posting = false
	_ = a.persistLocked()
}

// getById confirmation protects against commenting on a not-yet-published post.
// Network errors leave the state unchanged for a later read-only status check.
func (a *app) verifyPublishedPost(id string, postID int64, token, group string) {
	ctx, cancel := context.WithTimeout(context.Background(), 22*time.Second)
	defer cancel()
	raw, err := vkCall(ctx, token, "wall.getById", url.Values{"posts": {"-" + group + "_" + strconv.FormatInt(postID, 10)}})
	if err != nil {
		a.mu.Lock()
		a.posting = false
		a.mu.Unlock()
		return
	}
	var posts []struct {
		ID       int64  `json:"id"`
		PostType string `json:"post_type"`
		Date     int64  `json:"date"`
	}
	if json.Unmarshal(raw, &posts) != nil || len(posts) == 0 || posts[0].ID != postID || posts[0].PostType == "postpone" || posts[0].Date > time.Now().UTC().Unix() {
		a.mu.Lock()
		a.posting = false
		a.mu.Unlock()
		return
	}
	// Be conservative: only a normal, already published wall post qualifies.
	if posts[0].PostType != "post" && posts[0].PostType != "copy" {
		a.mu.Lock()
		a.posting = false
		a.mu.Unlock()
		return
	}
	a.mu.Lock()
	record := a.getItemLocked(id)
	if record == nil || record.State != "vk_postponed" || record.VKPostID != postID {
		a.posting = false
		a.mu.Unlock()
		return
	}
	record.State = "published"
	record.PublishedAt = time.Unix(posts[0].Date, 0).UTC()
	record.CommentState = "sending"
	comment := record.Comment
	a.auditLocked("vk_confirm_published", id, "VK подтвердил выход отложенного поста")
	_ = a.persistLocked()
	a.mu.Unlock()
	cid, cErr := sendSourceComment(ctx, token, group, postID, comment, id)
	a.mu.Lock()
	defer a.mu.Unlock()
	if record = a.getItemLocked(id); record != nil {
		if cErr != nil {
			record.CommentState = "check"
			record.Error = "Публикация подтверждена. Проверь первый комментарий в VK вручную: " + cErr.Error()
		} else {
			record.CommentState = "sent"
			record.CommentID = cid
			record.Error = ""
			a.auditLocked("vk_first_comment", id, "Ссылки добавлены")
		}
	}
	a.posting = false
	_ = a.persistLocked()
}
func (a *app) retryVKQueue(id string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	it := a.getItemLocked(id)
	if it == nil || it.State != "vk_pending_error" || it.VKPostID > 0 {
		return errors.New("Повтор доступен только до попытки создания поста VK")
	}
	it.State = "vk_pending"
	it.VKQueueError = ""
	return a.persistLocked()
}
func (a *app) cancelVKQueue(id string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	it := a.getItemLocked(id)
	if it == nil || (it.State != "vk_pending" && it.State != "vk_pending_error") || it.VKPostID > 0 {
		return errors.New("Снять можно только локальную очередь, ещё не отправленную в VK")
	}
	it.State = "approved"
	it.ScheduledAt = time.Time{}
	it.VKQueueError = ""
	a.auditLocked("vk_queue_cancel", it.ID, "Локальная очередь отменена")
	return a.persistLocked()
}

// enqueueExistingApproved is an explicit action for already-approved legacy drafts.
// A migration or application start must never bulk-send old content to VK.
func (a *app) enqueueExistingApproved(id string) (time.Time, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	it := a.getItemLocked(id)
	if it == nil || it.State != "approved" || it.VKPostID > 0 {
		return time.Time{}, errors.New("В отложку можно отправить только одобренный пост без VK ID")
	}
	if !a.state.Config.VKPostponedOnApproval {
		return time.Time{}, errors.New("Включи отложенные посты VK в настройках")
	}
	if !validGroup(a.state.Config.GroupID) {
		return time.Time{}, errors.New("Сначала укажи числовой ID сообщества VK")
	}
	if !it.Verified || len(it.Images) < 3 || len(it.Images) > 7 || !hasGameHashtags(it.Draft, it.Game) || strings.TrimSpace(it.Comment) == "" {
		return time.Time{}, errors.New("Нужны проверка первоисточника, 3–7 изображений, обязательный хэштег и ссылки")
	}
	when, err := a.nextSlotLocked(time.Now().UTC().Add(10*time.Minute), id)
	if err != nil {
		return time.Time{}, err
	}
	it.ScheduledAt = when
	it.VKQueueError = ""
	it.Error = ""
	it.State = "vk_pending"
	a.auditLocked("vk_manual_enqueue", id, "Явно поставлен в очередь отложки VK: "+when.In(moscow).Format("02.01.2006 15:04"))
	return when, a.persistLocked()
}
