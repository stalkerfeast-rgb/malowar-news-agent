package main

import (
	"regexp"
	"strings"
	"unicode"
)

var editorialTagRE = regexp.MustCompile(`#[\p{L}\p{N}_]+`)

// VK posts use four or five hashtags in a final, separate line.
// The model may omit them, so we normalise its draft, but never silently edit
// something the human has already approved or published.
func hasMandatoryHashtags(draft string) bool {
	lines := strings.Split(strings.TrimSpace(draft), "\n")
	if len(lines) == 0 {
		return false
	}
	last := strings.TrimSpace(lines[len(lines)-1])
	tags := editorialTagRE.FindAllString(last, -1)
	if len(tags) < 4 || len(tags) > 5 {
		return false
	}
	residual := strings.TrimSpace(editorialTagRE.ReplaceAllString(last, ""))
	if residual != "" {
		return false
	}
	seen := map[string]bool{}
	for _, tag := range tags {
		t := strings.ToLower(tag)
		if seen[t] {
			return false
		}
		seen[t] = true
	}
	return true
}
func ensureMandatoryHashtags(draft, category string) string {
	draft = strings.TrimSpace(draft)
	if draft == "" || hasMandatoryHashtags(draft) {
		return draft
	}
	lines := strings.Split(draft, "\n")
	last := strings.TrimSpace(lines[len(lines)-1])
	existing := []string{}
	if last != "" && strings.TrimSpace(editorialTagRE.ReplaceAllString(last, "")) == "" {
		existing = editorialTagRE.FindAllString(last, -1)
		lines = lines[:len(lines)-1]
	}
	seen := map[string]bool{}
	tags := []string{}
	for _, tag := range existing {
		key := strings.ToLower(tag)
		if !seen[key] && len(tags) < 5 {
			tags = append(tags, tag)
			seen[key] = true
		}
	}
	defaults := []string{"#Стратегии", "#Игры", "#MalowarGaming", "#НовостиИгр", "#Steam"}
	if strings.Contains(strings.ToLower(category), "мод") {
		defaults = []string{"#Моды", "#Стратегии", "#Игры", "#MalowarGaming", "#SteamWorkshop"}
	}
	for _, tag := range defaults {
		if len(tags) >= 4 {
			break
		}
		key := strings.ToLower(tag)
		if !seen[key] {
			tags = append(tags, tag)
			seen[key] = true
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n")) + "\n\n" + strings.Join(tags, " ")
}

// gameHashtag joins every letter and digit in the official game name, preserving
// Unicode and the game's original spelling (Age of Wonders 4 -> #AgeofWonders4).
// It never guesses an abbreviation or invents a different title.
func gameHashtag(game string) string {
	var out strings.Builder
	out.WriteByte('#')
	for _, r := range strings.TrimSpace(game) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
			out.WriteRune(r)
		}
	}
	if out.Len() == 1 {
		return ""
	}
	return out.String()
}

func hasGameHashtags(draft, game string) bool {
	if !hasMandatoryHashtags(draft) {
		return false
	}
	target := gameHashtag(game)
	if target == "" {
		return false
	}
	lines := strings.Split(strings.TrimSpace(draft), "\n")
	for _, tag := range editorialTagRE.FindAllString(lines[len(lines)-1], -1) {
		if strings.EqualFold(tag, target) {
			return true
		}
	}
	return false
}

// Normalise only new, unapproved AI text. Never mutate existing approved posts.
func ensureGameHashtags(draft, category, game string) string {
	draft = strings.TrimSpace(draft)
	if draft == "" || hasGameHashtags(draft, game) {
		return draft
	}
	target := gameHashtag(game)
	if target == "" {
		return ensureMandatoryHashtags(draft, category)
	}
	lines := strings.Split(draft, "\n")
	existing := []string{}
	last := strings.TrimSpace(lines[len(lines)-1])
	if strings.TrimSpace(editorialTagRE.ReplaceAllString(last, "")) == "" {
		existing = editorialTagRE.FindAllString(last, -1)
		lines = lines[:len(lines)-1]
	}
	seen := map[string]bool{strings.ToLower(target): true}
	tags := []string{target}
	defaults := []string{"#Стратегии", "#MalowarGaming", "#НовостиИгр", "#Игры", "#Steam"}
	if strings.Contains(strings.ToLower(category), "мод") {
		defaults = []string{"#Моды", "#Стратегии", "#MalowarGaming", "#SteamWorkshop", "#Игры"}
	}
	for _, tag := range append(existing, defaults...) {
		key := strings.ToLower(tag)
		if !seen[key] && len(tags) < 5 {
			tags = append(tags, tag)
			seen[key] = true
		}
		if len(tags) == 5 {
			break
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n")) + "\n\n" + strings.Join(tags, " ")
}
