package main

import (
	"errors"
	"net/url"
	"regexp"
	"strings"
)

var vkCommunitySegment = regexp.MustCompile(`^[a-zA-Z0-9_.-]{2,70}$`)

// Accept only official community pages, never arbitrary websites or VK API URLs.
func normalizeVKCommunity(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		raw = "https://vk.ru/malowargaming"
	}
	if !strings.Contains(raw, "://") {
		raw = "https://vk.ru/" + strings.TrimLeft(raw, "@/")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || (u.Hostname() != "vk.ru" && u.Hostname() != "vk.com") || u.Port() != "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("Укажи обычную ссылку на сообщество https://vk.ru/имя или https://vk.com/имя")
	}
	seg := strings.Trim(u.Path, "/")
	if !vkCommunitySegment.MatchString(seg) || strings.Contains(seg, "/") || strings.EqualFold(seg, "feed") || strings.EqualFold(seg, "login") {
		return "", errors.New("В ссылке VK должно быть только короткое имя сообщества или public123/club123")
	}
	return "https://vk.ru/" + seg, nil
}
func (a *app) vkCommunityURL() string {
	a.mu.Lock()
	raw := a.state.Config.VKCommunityURL
	a.mu.Unlock()
	v, err := normalizeVKCommunity(raw)
	if err != nil {
		return "https://vk.ru/malowargaming"
	}
	return v
}
