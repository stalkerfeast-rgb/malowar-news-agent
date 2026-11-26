package main

import (
	"strings"
	"unicode"
)

// Compare only text observed from two official source snapshots. This is NOT a changelog.
func excerptDifference(old, new string) string {
	if old == "" || new == "" || old == new {
		return ""
	}
	tokenize := func(text string) []string {
		return strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	}
	before := map[string]bool{}
	for _, word := range tokenize(old) {
		if len([]rune(word)) > 4 {
			before[word] = true
		}
	}
	additions := []string{}
	seen := map[string]bool{}
	for _, word := range tokenize(new) {
		if len([]rune(word)) > 4 && !before[word] && !seen[word] {
			additions = append(additions, word)
			seen[word] = true
			if len(additions) >= 22 {
				break
			}
		}
	}
	if len(additions) == 0 {
		return "Описания отличаются, но существенные новые слова не выделены. Сверь изменения вручную."
	}
	return "Сравнение текстов описаний (не официальный changelog). Новые слова: " + strings.Join(additions, ", ")
}

func (a *app) enrichBeforeInsertLocked(it *Item) {
	if it.Category != "Моды" {
		return
	}
	for _, prev := range a.state.Items {
		if prev.Game == it.Game && prev.Source == it.Source && prev.ID != it.ID && prev.Time.Before(it.Time) {
			if it.DiffSummary == "" {
				it.DiffSummary = excerptDifference(prev.Excerpt, it.Excerpt)
			}
			return
		}
	}
}
