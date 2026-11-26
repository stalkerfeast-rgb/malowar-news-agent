package main

import (
	"regexp"
	"strings"
)

var linkPattern = regexp.MustCompile(`(?i)(?:https?://|www\.)[^\s<>"']+`)

func extractLinks(input string) (string, []string) {
	links := []string{}
	body := linkPattern.ReplaceAllStringFunc(input, func(match string) string {
		clean := strings.TrimRight(match, ".,;:!?)]}»")
		if clean != "" {
			links = append(links, clean)
		}
		return ""
	})
	// Clean empty source link lines when migrated from an older version.
	lines := []string{}
	for _, l := range strings.Split(body, "\n") {
		if strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(l), ":")) == "Источник" {
			continue
		}
		lines = append(lines, l)
	}
	return strings.TrimSpace(strings.Join(lines, "\n")), links
}
func addOnce(list *[]string, found map[string]bool, link string) {
	link = strings.TrimSpace(link)
	if link == "" || found[link] {
		return
	}
	found[link] = true
	*list = append(*list, link)
}
func defaultComment(i Item) string {
	if i.URL == "" {
		return "Источники и ссылки:"
	}
	return "Источники и ссылки:\n" + i.URL
}
func separateLinks(draft, comment, source string) (string, string) {
	text, fromDraft := extractLinks(draft)
	_, inComment := extractLinks(comment)
	found := map[string]bool{}
	links := []string{}
	for _, l := range inComment {
		addOnce(&links, found, l)
	}
	for _, l := range fromDraft {
		addOnce(&links, found, l)
	}
	addOnce(&links, found, source)
	// Keep manually-written non-link context from the comment.
	commentPlain, _ := extractLinks(comment)
	commentPlain = strings.TrimSpace(commentPlain)
	if commentPlain == "" {
		commentPlain = "Источники и ссылки:"
	}
	if len(links) > 0 {
		commentPlain += "\n" + strings.Join(links, "\n")
	}
	return text, strings.TrimSpace(commentPlain)
}
