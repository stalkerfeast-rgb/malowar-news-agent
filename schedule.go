package main

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

var moscow = time.FixedZone("MSK", 3*60*60)

func validateSlots(slots string) ([]int, error) {
	if strings.TrimSpace(slots) == "" {
		slots = "10:00,14:00,18:00,21:00"
	}
	found := map[int]bool{}
	for _, piece := range strings.Split(slots, ",") {
		t, err := time.Parse("15:04", strings.TrimSpace(piece))
		if err != nil {
			return nil, fmt.Errorf("Неверный слот %q. Используй ЧЧ:ММ через запятую", piece)
		}
		minutes := t.Hour()*60 + t.Minute()
		if found[minutes] {
			return nil, fmt.Errorf("Повтор времени %q", piece)
		}
		found[minutes] = true
	}
	if len(found) == 0 || len(found) > 12 {
		return nil, errors.New("Укажи от 1 до 12 временных слотов")
	}
	result := make([]int, 0, len(found))
	for v := range found {
		result = append(result, v)
	}
	sort.Ints(result)
	return result, nil
}

func (a *app) occupiedLocked(t time.Time, ignore string) bool {
	for _, it := range a.state.Items {
		if it.ID == ignore || it.ScheduledAt.IsZero() || (it.State != "scheduled" && it.State != "vk_pending" && it.State != "vk_pending_error" && it.State != "vk_sending" && it.State != "vk_postponed" && !(a.state.Config.VKBrowserMode && (it.State == "approved" || it.State == "vk_browser_marked"))) {
			continue
		}
		d := it.ScheduledAt.Sub(t)
		if d < 0 {
			d = -d
		}
		if d < 30*time.Minute {
			return true
		}
	}
	return false
}

func (a *app) nextSlotLocked(from time.Time, ignore string) (time.Time, error) {
	slots, err := validateSlots(a.state.Config.PostSlots)
	if err != nil {
		return time.Time{}, err
	}
	start := from.In(moscow)
	for day := 0; day < 31; day++ {
		date := start.AddDate(0, 0, day)
		for _, m := range slots {
			candidate := time.Date(date.Year(), date.Month(), date.Day(), m/60, m%60, 0, 0, moscow).UTC()
			if candidate.After(from.Add(1*time.Minute)) && !a.occupiedLocked(candidate, ignore) {
				return candidate, nil
			}
		}
	}
	return time.Time{}, errors.New("Нет свободного времени в ближайшие 30 дней")
}

func (a *app) setSchedule(id, operation, localDate string) (time.Time, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	it := a.getItemLocked(id)
	if it == nil {
		return time.Time{}, errors.New("Новость не найдена")
	}
	if it.State != "approved" && it.State != "scheduled" && it.State != "missed" {
		return time.Time{}, errors.New("Сначала проверь и одобри пост")
	}
	if !it.Verified || len(it.Images) < 3 || len(it.Images) > 7 {
		return time.Time{}, errors.New("Нужны проверка фактов и 3–7 изображений")
	}
	var date time.Time
	now := time.Now().UTC()
	switch operation {
	case "cancel":
		it.State = "approved"
		it.ScheduledAt = time.Time{}
		it.Error = ""
		a.auditLocked("cancel_schedule", it.ID, "Снято с расписания")
		return date, a.persistLocked()
	case "queue":
		var err error
		date, err = a.nextSlotLocked(now, id)
		if err != nil {
			return date, err
		}
	case "at":
		parsed, err := time.ParseInLocation("2006-01-02T15:04", localDate, moscow)
		if err != nil {
			return date, errors.New("Укажи дату и время по Москве")
		}
		date = parsed.UTC()
		if a.occupiedLocked(date, id) {
			return date, errors.New("В пределах 30 минут уже есть другой запланированный пост")
		}
	default:
		return date, errors.New("Неверный режим планирования")
	}
	if !date.After(now.Add(1*time.Minute)) || date.After(now.Add(31*24*time.Hour)) {
		return date, errors.New("Время должно быть в будущем, в пределах 31 дня")
	}
	if a.state.Config.VKBrowserMode {
		// Planning only; the user will choose the time in the official VK editor.
		it.State = "approved"
		it.ScheduledAt = date
		it.VKBrowserPreparedAt = time.Time{}
		it.VKBrowserExportDir = ""
		it.VKBrowserFingerprint = ""
		a.auditLocked("vk_browser_plan", it.ID, "Предложенное время для ручного выбора в VK: "+date.In(moscow).Format("02.01.2006 15:04"))
		return date, a.persistLocked()
	}
	if a.state.Config.VKPostponedOnApproval {
		if !validGroup(a.state.Config.GroupID) {
			return time.Time{}, errors.New("Сначала укажи ID сообщества в настройках VK")
		}
		if !hasGameHashtags(it.Draft, it.Game) || strings.TrimSpace(it.Comment) == "" {
			return time.Time{}, errors.New("Нужны обязательный хэштег игры и ссылки в первом комментарии")
		}
		it.State = "vk_pending"
		it.ScheduledAt = date
		it.Error = ""
		it.VKQueueError = ""
		a.auditLocked("vk_schedule", it.ID, "Отправка в отложку VK запрошена на "+date.In(moscow).Format("02.01.2006 15:04")+" МСК")
		return date, a.persistLocked()
	}
	it.State = "scheduled"
	it.ScheduledAt = date
	it.Error = ""
	a.auditLocked("schedule", it.ID, "Запланировано локально на "+date.In(moscow).Format("02.01.2006 15:04")+" МСК")
	return date, a.persistLocked()
}

// processSchedule only sends posts explicitly scheduled and previously approved by the editor.
// Missed slots are NOT silently replayed after a PC shutdown or missing VK credentials.
func (a *app) processSchedule(now time.Time) {
	a.mu.Lock()
	if a.state.Config.VKBrowserMode {
		a.mu.Unlock()
		return
	}
	if a.posting {
		a.mu.Unlock()
		return
	}
	var due *Item
	for _, it := range a.state.Items {
		if it.State != "scheduled" || it.ScheduledAt.IsZero() || it.ScheduledAt.After(now) {
			continue
		}
		if due == nil || it.ScheduledAt.Before(due.ScheduledAt) {
			due = it
		}
	}
	if due == nil {
		a.mu.Unlock()
		return
	}
	if now.Sub(due.ScheduledAt) > 45*time.Minute || a.token == "" || !validGroup(a.state.Config.GroupID) || !due.Verified || len(due.Images) < 3 || len(due.Images) > 7 {
		due.State = "missed"
		due.Error = "Срок пропущен или VK не подключён. Проверь актуальность новости и назначь новое время."
		a.auditLocked("missed", due.ID, due.Error)
		_ = a.persistLocked()
		a.mu.Unlock()
		return
	}
	id := due.ID
	a.mu.Unlock()
	a.publish(id)
}

func (a *app) auditLocked(action, id, details string) {
	a.state.Audit = append(a.state.Audit, AuditEvent{At: time.Now().UTC(), Action: action, ItemID: id, Details: details})
	if len(a.state.Audit) > 1200 {
		a.state.Audit = a.state.Audit[len(a.state.Audit)-1200:]
	}
}
