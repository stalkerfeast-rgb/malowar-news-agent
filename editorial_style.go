package main

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// editorialHeadline avoids rendering "Game: Game ..." in source-only drafts.
// A title that already begins with the game's name remains intact.
func editorialHeadline(game, title string) string {
	game, title = strings.TrimSpace(game), strings.TrimSpace(title)
	if game == "" {
		return title
	}
	if title == "" {
		return game
	}
	if strings.HasPrefix(strings.ToLower(title), strings.ToLower(game)) {
		suffix := title[len(game):]
		if suffix == "" || strings.HasPrefix(suffix, " ") || strings.HasPrefix(suffix, ":") || strings.HasPrefix(suffix, "—") || strings.HasPrefix(suffix, "-") || strings.HasPrefix(suffix, "–") {
			return title
		}
	}
	return game + ": " + title
}

// redundantDefinition matches only a vacuous game/genre definition, not real
// feature announcements or ordinary mentions of the game in a news paragraph.
func redundantDefinition(sentence, game string) bool {
	if strings.TrimSpace(game) == "" || utf8.RuneCountInString(sentence) > 100 || strings.ContainsAny(sentence, ",;:") {
		return false
	}
	pattern := `(?i)^\s*[«"']?` + regexp.QuoteMeta(strings.TrimSpace(game)) + `[»"']?\s*(?:[-—–:]\s*)?(?:это\s+|является\s+|представляет\s+собой\s+)`
	if !regexp.MustCompile(pattern).MatchString(sentence) {
		return false
	}
	lower := strings.ToLower(sentence)
	for _, factual := range []string{"обнов", "добав", "нов", "появ", "измен", "доступ", "планир", "разработ", "позволя"} {
		if strings.Contains(lower, factual) {
			return false
		}
	}
	// Strip only a pure, generic genre-definition sentence. Never discard
	// facts about dates, gameplay features, numbers, updates, or platforms.
	for _, genre := range []string{"игра", "стратеги", "симулятор", "проект", "тактик"} {
		if strings.Contains(lower, genre) && !strings.ContainsAny(sentence, "0123456789") {
			return true
		}
	}
	return false
}

// polishEditorialText safely removes a common AI-generated filler opening,
// provided substantive text remains after it. Never touch the headline.
func polishEditorialText(raw, game string) string {
	raw = strings.TrimSpace(strings.ReplaceAll(raw, "\r\n", "\n"))
	if raw == "" {
		return ""
	}
	lines := strings.Split(raw, "\n")
	if len(lines) < 3 {
		return raw
	}
	for idx := 1; idx < len(lines); idx++ {
		line := strings.TrimSpace(lines[idx])
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			break
		}
		dot := strings.Index(line, ". ")
		if dot < 0 {
			break
		}
		intro, remainder := strings.TrimSpace(line[:dot+1]), strings.TrimSpace(line[dot+2:])
		if redundantDefinition(intro, game) && utf8.RuneCountInString(remainder) >= 50 {
			lines[idx] = remainder
		}
		break
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// editorialQualityWarnings complement source verification; they are prompts
// for human editorial review, not automated fact-check approval.
func editorialQualityWarnings(game, source, output, variant string) []string {
	warnings := []string{}
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) < 2 {
		return append(warnings, "Нужны отдельный заголовок и содержательный текст новости")
	}
	title := strings.TrimSpace(lines[0])
	prefix := strings.ToLower(strings.TrimSpace(game))
	if prefix != "" && strings.HasPrefix(strings.ToLower(title), prefix) {
		// Check a repeated adjacent game title only, not legitimate words later in the headline.
		after := strings.TrimSpace(runeToString(title, utf8.RuneCountInString(game)))
		if strings.HasPrefix(after, ":") || strings.HasPrefix(after, "—") || strings.HasPrefix(after, "-") || strings.HasPrefix(after, "–") {
			after = strings.TrimLeft(strings.TrimSpace(after), ":—–- ")
			if strings.HasPrefix(strings.ToLower(after), prefix) {
				warnings = append(warnings, "Заголовок повторяет название игры подряд; убери дубль")
			}
		}
	}
	bodyStart := ""
	for _, ln := range lines[1:] {
		ln = strings.TrimSpace(ln)
		if ln != "" && !strings.HasPrefix(ln, "#") {
			bodyStart = ln
			break
		}
	}
	if bodyStart != "" {
		firstSentence := bodyStart
		if dot := strings.Index(firstSentence, ". "); dot >= 0 {
			firstSentence = firstSentence[:dot+1]
		}
		if redundantDefinition(firstSentence, game) {
			warnings = append(warnings, "Первый абзац повторяет игру и объясняет её жанр вместо самой новости")
		}
	}
	size, sourceSize := utf8.RuneCountInString(output), utf8.RuneCountInString(source)
	desired := 350
	if variant == "expanded" {
		desired = 650
	}
	if size < desired && sourceSize >= 260 {
		warnings = append(warnings, fmt.Sprintf("Текст выглядит слишком коротким (%d зн.) для информативного источника: раскрой подтверждённые детали", size))
	}
	if sourceSize < 160 {
		warnings = append(warnings, "В источнике мало подробностей. Для содержательного поста открой первоисточник; ИИ не должен додумывать факты")
	}
	return warnings
}

func runeToString(s string, offset int) string {
	r := []rune(s)
	if offset >= len(r) {
		return ""
	}
	return string(r[offset:])
}
