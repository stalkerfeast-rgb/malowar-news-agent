package main

import (
	"strings"
	"unicode"
)

func scoreNews(it Item) int {
	base := map[string]int{"Релизы": 83, "DLC": 76, "Моды": 59, "Обновления": 56, "Новости": 48}[it.Category]
	if base == 0 {
		base = 48
	}
	s := strings.ToLower(it.Title + " " + it.Excerpt)
	add := func(terms []string, weight int) {
		for _, w := range terms {
			if strings.Contains(s, w) {
				base += weight
				break
			}
		}
	}
	add([]string{"announc", "reveal", "анонс", "объявили"}, 12)
	add([]string{"major update", "large update", "expansion", "крупное обновление"}, 10)
	add([]string{"gameplay", "геймплей", "demo", "демоверс"}, 7)
	add([]string{"release date", "date reveal", "дата выхода"}, 8)
	add([]string{"hotfix", "minor fix", "small patch", "исправление ошибок"}, -30)
	add([]string{"beta branch", "maintenance", "technical update"}, -8)
	title := strings.ToLower(it.Game)
	for _, game := range []string{"stellaris", "crusader kings", "europa universalis", "victoria 3", "total war", "age of wonders", "hearts of iron", "warhammer", "age of empires"} {
		if strings.Contains(title, game) {
			base += 7
			break
		}
	}
	if base < 1 {
		base = 1
	}
	if base > 100 {
		base = 100
	}
	return base
}

func normalizeNewsFingerprint(v string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(v) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}
