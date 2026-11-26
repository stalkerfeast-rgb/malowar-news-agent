package main

import (
	"bytes"
	"errors"
	"fmt"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Candidate awards are an editorial queue, NOT falsely labeled as approved artwork.
// The three previously approved PNG assets remain immutable and bundled.
type CatalogAward struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Category    string `json:"category"`
	Status      string `json:"status"` // ready: has editor-provided artwork, candidate: needs artwork
	Description string `json:"description,omitempty"`
}

var awardIDs = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)

func defaultAwardCatalog() []CatalogAward {
	names := []struct{ ID, Name, Category string }{
		{"pearl", "Скрытая жемчужина", "Открытия"},
		{"world", "Живой мир", "Атмосфера"},
		{"doubt", "Сомнительно", "Критика"},
		{"editors_choice", "Выбор редакции", "Редакция"},
		{"talented_debut", "Талантливый дебют", "Открытия"},
		{"unusual_mechanic", "Необычная механика", "Геймплей"},
		{"great_soundtrack", "Великолепный саундтрек", "Звук"},
		{"optimization_trouble", "Проблемы с оптимизацией", "Критика"},
		{"master_tactician", "Мастер тактики", "Геймплей"},
		{"grand_campaign", "Большая кампания", "Масштаб"},
		{"deep_economy", "Глубокая экономика", "Геймплей"},
		{"living_diplomacy", "Живая дипломатия", "Геймплей"},
		{"challenging_ai", "Достойный противник", "Геймплей"},
		{"fresh_take", "Свежий взгляд", "Открытия"},
		{"indie_discovery", "Инди-открытие", "Открытия"},
		{"rising_star", "Восходящая звезда", "Открытия"},
		{"early_promise", "Большие перспективы", "Открытия"},
		{"strong_start", "Уверенный старт", "Открытия"},
		{"strategic_depth", "Глубина стратегии", "Геймплей"},
		{"clever_logistics", "Тонкая логистика", "Геймплей"},
		{"fierce_battles", "Жаркие сражения", "Сражения"},
		{"siege_master", "Искусство осады", "Сражения"},
		{"naval_power", "Морская мощь", "Сражения"},
		{"space_odyssey", "Космическая одиссея", "Масштаб"},
		{"epic_scope", "Эпический масштаб", "Масштаб"},
		{"rich_lore", "Богатый лор", "Атмосфера"},
		{"strong_story", "Сильный сюжет", "Атмосфера"},
		{"great_style", "Выразительный стиль", "Атмосфера"},
		{"memorable_world", "Запоминающийся мир", "Атмосфера"},
		{"historical_care", "Внимание к истории", "Атмосфера"},
		{"sound_design", "Мастерство звука", "Звук"},
		{"good_localization", "Отличный перевод", "Локализация"},
		{"russian_voice", "Русская озвучка", "Локализация"},
		{"community_favorite", "Любимец сообщества", "Сообщество"},
		{"modders_dream", "Мечта мододела", "Моды"},
		{"mod_of_month", "Мод месяца", "Моды"},
		{"endless_replay", "Можно переигрывать", "Геймплей"},
		{"one_more_turn", "Ещё один ход", "Геймплей"},
		{"coop_highlight", "Кооперативный хит", "Сообщество"},
		{"smart_tutorial", "Хорошее обучение", "Удобство"},
		{"accessible_depth", "Просто начать", "Удобство"},
		{"technical_polish", "Технический блеск", "Техника"},
		{"frequent_updates", "Забота разработчиков", "Поддержка"},
		{"roadmap_worth", "Обещания с перспективой", "Поддержка"},
		{"balance_trouble", "Проблемы баланса", "Критика"},
		{"rough_launch", "Сложный старт", "Критика"},
		{"content_shortage", "Мало контента", "Критика"},
		{"interface_trouble", "Неудобный интерфейс", "Критика"},
		{"high_price", "Спорная цена", "Критика"},
		{"worthy_return", "Возвращение легенды", "Редакция"},
	}
	out := make([]CatalogAward, 0, len(names))
	for i, v := range names {
		status := "candidate"
		if i < 3 {
			status = "ready"
		}
		out = append(out, CatalogAward{ID: v.ID, Name: v.Name, Category: v.Category, Status: status})
	}
	return out
}
func (a *app) awardCatalogLocked(id string) *CatalogAward {
	for n := range a.state.AwardCatalog {
		if a.state.AwardCatalog[n].ID == id {
			return &a.state.AwardCatalog[n]
		}
	}
	return nil
}
func (a *app) addAwardCandidate(name, category string) error {
	name = strings.TrimSpace(name)
	category = strings.TrimSpace(category)
	if name == "" || len([]rune(name)) > 65 {
		return errors.New("Название награды должно содержать от 1 до 65 символов")
	}
	if len([]rune(category)) > 40 {
		return errors.New("Слишком длинная категория")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	id := fmt.Sprintf("custom_%04d", len(a.state.AwardCatalog)+1)
	for a.awardCatalogLocked(id) != nil {
		id += "x"
	}
	a.state.AwardCatalog = append(a.state.AwardCatalog, CatalogAward{ID: id, Name: name, Category: category, Status: "candidate"})
	return a.persistLocked()
}
func (a *app) awardPNG(id string) ([]byte, error) {
	if !awardIDs.MatchString(id) {
		return nil, errors.New("Недопустимый ID награды")
	}
	a.mu.Lock()
	award := a.awardCatalogLocked(id)
	ready := award != nil && award.Status == "ready"
	a.mu.Unlock()
	if !ready {
		return nil, errors.New("Графика награды ещё не утверждена")
	}
	if id == "pearl" || id == "world" || id == "doubt" {
		return webFiles.ReadFile("app/awards/" + id + ".png")
	}
	return os.ReadFile(filepath.Join(a.dataDir, "awards", id+".png"))
}
func (a *app) uploadAwardPNG(id string, src io.Reader) error {
	if !awardIDs.MatchString(id) || id == "pearl" || id == "world" || id == "doubt" {
		return errors.New("Утверждённые три исходные плашки нельзя заменять")
	}
	a.mu.Lock()
	award := a.awardCatalogLocked(id)
	exists := award != nil
	a.mu.Unlock()
	if !exists {
		return errors.New("Награда не найдена")
	}
	data, err := io.ReadAll(io.LimitReader(src, (8<<20)+1))
	if err != nil {
		return err
	}
	if len(data) > 8<<20 {
		return errors.New("PNG должен быть меньше 8 МБ")
	}
	config, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width < 300 || config.Height < 40 || config.Width > 4096 || config.Height > 2048 {
		return errors.New("Нужна горизонтальная плашка PNG разрешением не меньше 300×40")
	}
	dir := filepath.Join(a.dataDir, "awards")
	if err = os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	target := filepath.Join(dir, id+".png")
	tmp := target + ".tmp"
	if err = os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	if err = os.Rename(tmp, target); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if record := a.awardCatalogLocked(id); record != nil {
		record.Status = "ready"
	}
	a.auditLocked("award_asset", id, "Добавлен редакционный PNG")
	return a.persistLocked()
}
