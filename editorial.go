package main

import (
	"errors"
	"strings"
)

// Editorial groups describe which audience a game belongs to. Event kinds
// describe what happened. Both are views of ONE stored news item.
const (
	bucketParadox   = "paradox"
	bucketMajor     = "major"
	bucketIndie     = "indie"
	bucketCandidate = "candidate"
	bucketOther     = "other"
)

func defaultCoreGames() []string {
	return []string{
		"Total War: Warhammer III", "Total War", "Age of Wonders 4", "Age of Wonders",
		"Civilization", "RimWorld", "Age of Empires IV", "Age of Empires",
		"Warhammer 40,000: Rogue Trader", "Warhammer 40,000", "The Settlers",
		"Frostpunk", "Manor Lords", "Company of Heroes", "Stronghold", "StarCraft",
	}
}

var paradoxGames = []string{
	"Stellaris", "Hearts of Iron IV", "Hearts of Iron 4", "HOI4",
	"Crusader Kings III", "Crusader Kings 3", "CK3", "Victoria 3", "Victoria III",
	"Europa Universalis V", "Europa Universalis 5", "EU5", "Europa Universalis IV",
	"Europa Universalis 4", "EU4", "Imperator: Rome", "Cities: Skylines", "Cities: Skylines II",
	// Note: Triumph games stay in major strategies even if published by Paradox.
}

func normalizeGameName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.NewReplacer("—", " ", "–", " ", "-", " ", ":", " ", "'", "", ".", " ", "_", " ").Replace(s)
	return strings.Join(strings.Fields(s), " ")
}
func gameMatches(game string, names []string) bool {
	key := normalizeGameName(game)
	if key == "" {
		return false
	}
	for _, n := range names {
		norm := normalizeGameName(n)
		// Match exact names and recognized franchise prefix followed by a word boundary.
		// Never match substrings such as 'Age' inside an unrelated game name.
		if key == norm || (len(norm) >= 8 && strings.HasPrefix(key, norm+" ")) {
			return true
		}
	}
	return false
}
func hasWorkshopKind(it Item) bool {
	return it.Source == "Workshop" || it.Source == "GitHub Releases" || it.Category == "Моды"
}
func editorialGame(it Item) string {
	if p := strings.TrimSpace(it.ParentGame); p != "" {
		return p
	}
	return strings.TrimSpace(it.Game)
}
func validEditorialBucket(b string) bool {
	switch b {
	case "", bucketParadox, bucketMajor, bucketIndie, bucketCandidate, bucketOther:
		return true
	}
	return false
}
func editorialBucket(it Item, c Config) string {
	if validEditorialBucket(it.EditorialOverride) && it.EditorialOverride != "" {
		return it.EditorialOverride
	}
	name := editorialGame(it)
	if gameMatches(name, paradoxGames) {
		return bucketParadox
	}
	if gameMatches(name, c.IndieGames) {
		return bucketIndie
	}
	if gameMatches(name, c.CoreGames) {
		return bucketMajor
	}
	return bucketCandidate // A candidate is NOT asserted to be indie until reviewed.
}
func eventKind(it Item) string {
	if hasWorkshopKind(it) {
		return "mod"
	}
	title := strings.ToLower(it.Title)
	if strings.Contains(title, "early access") || strings.Contains(title, "ранний доступ") {
		return "early_access"
	}
	if strings.Contains(title, "dlc") || strings.Contains(title, "expansion") || strings.Contains(title, "дополнен") || it.Category == "DLC" {
		return "dlc"
	}
	if strings.Contains(title, "release") || strings.Contains(title, "launch") || strings.Contains(title, "вышл") || strings.Contains(title, "релиз") || strings.Contains(title, "out now") || it.Category == "Релизы" {
		return "release"
	}
	if it.Category == "Обновления" || strings.Contains(title, "patch") || strings.Contains(title, "update") || strings.Contains(title, "hotfix") || strings.Contains(title, "обновлен") {
		return "update"
	}
	return "news"
}
func editorialConfigValid(games []string) bool {
	if len(games) > 100 {
		return false
	}
	for _, g := range games {
		if len([]rune(g)) > 90 || strings.TrimSpace(g) == "" {
			return false
		}
	}
	return true
}
func (a *app) setEditorialTag(id, bucket, parentGame string) error {
	if !validEditorialBucket(bucket) {
		return errors.New("Неизвестная редакционная рубрика")
	}
	parentGame = strings.TrimSpace(parentGame)
	if len([]rune(parentGame)) > 90 {
		return errors.New("Слишком длинное название игры")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	it := a.getItemLocked(id)
	if it == nil {
		return errors.New("Новость не найдена")
	}
	// Does not change status, verification, images or publication eligibility.
	it.ParentGame = parentGame
	it.EditorialOverride = bucket
	a.auditLocked("editorial", it.ID, "Рубрика: "+bucket+", игра: "+parentGame)
	return a.persistLocked()
}
