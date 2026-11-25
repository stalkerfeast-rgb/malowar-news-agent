package main

import (
	"context"
	"encoding/xml"
	"fmt"
	"regexp"
	"strings"
	"time"
)

const workshopItemURL = "https://steamcommunity.com/sharedfiles/filedetails/?id="
const workshopChangesURL = "https://steamcommunity.com/sharedfiles/filedetails/changelog/"

type workshopSourceLink struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}

var featuredWorkshopLinks = map[string][]workshopSourceLink{
	"2962333032": {{"Канал команды CK3AGOT", "https://www.youtube.com/channel/UCKXLrXe0NoJUQZHyYD5VNpw"}},
	"2887120253": {
		{"Файлы и версии на Nexus Mods", "https://www.nexusmods.com/crusaderkings3/mods/32?tab=files"},
		{"Пресс-кит Elder Kings II", "https://www.elderkings.com/index.php/press"},
		{"Скриншоты команды Elder Kings II", "https://elderkings.com/index.php/gallery"},
	},
}

var featuredWorkshop = []WorkshopSource{
	{FileID: "2962333032", Name: "A Game of Thrones", ParentGame: "Crusader Kings III"},
	{FileID: "2887120253", Name: "Elder Kings 2", ParentGame: "Crusader Kings III"},
}

// Keep explicitly configured names and parent games when they overlap the watchlist.
func workshopSources(configured []WorkshopSource) []WorkshopSource {
	sources := append([]WorkshopSource(nil), configured...)
	seen := make(map[string]bool, len(sources))
	for _, source := range sources {
		seen[source.FileID] = true
	}
	for _, source := range featuredWorkshop {
		if !seen[source.FileID] {
			sources = append(sources, source)
		}
	}
	return sources
}

func featuredWorkshopID(item Item) string {
	if item.Source != "Workshop" {
		return ""
	}
	for _, source := range featuredWorkshop {
		if item.URL == workshopItemURL+source.FileID {
			return source.FileID
		}
	}
	return ""
}

func workshopChangeText(ctx context.Context, id string, updated int64) string {
	body, err := get(ctx, workshopChangesURL+id+"?l=english", nil)
	if err != nil {
		return ""
	}
	return extractWorkshopChange(body, updated)
}

func extractWorkshopChange(body []byte, updated int64) string {
	pattern := regexp.MustCompile(fmt.Sprintf(`(?is)<p\s+id=["']%d["'][^>]*>(.*?)</p>`, updated))
	match := pattern.FindSubmatch(body)
	if len(match) != 2 {
		return ""
	}
	return plain(strings.NewReplacer("<br>", " ", "<br/>", " ", "<br />", " ").Replace(string(match[1])))
}

var modVersionPattern = regexp.MustCompile(`\b[0-9]+(?:\.[0-9]+){1,3}\b`)
var youtubeVideoID = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

func enrichWorkshopUpdate(ctx context.Context, id string, item *Item) {
	item.Excerpt = workshopChangeText(ctx, id, item.Time.Unix())
	if id != "2962333032" || item.Excerpt == "" {
		return
	}
	version := modVersionPattern.FindString(item.Excerpt)
	if version == "" {
		return
	}
	feed, err := get(ctx, "https://www.youtube.com/feeds/videos.xml?channel_id=UCKXLrXe0NoJUQZHyYD5VNpw", nil)
	if err != nil {
		return
	}
	if title, description, link := matchWorkshopVideo(feed, version, item.Time); link != "" {
		item.Excerpt += "\n\nМатериал команды о той же версии: " + title + "\n" + description
		item.WorkshopSources = append(item.WorkshopSources, workshopSourceLink{"Видео команды: " + title, link})
	}
}

func matchWorkshopVideo(data []byte, version string, updated time.Time) (string, string, string) {
	var feed struct {
		Entries []struct {
			ID        string    `xml:"videoId"`
			Title     string    `xml:"title"`
			Published time.Time `xml:"published"`
			Media     struct {
				Description string `xml:"description"`
			} `xml:"group"`
		} `xml:"entry"`
	}
	if xml.Unmarshal(data, &feed) != nil {
		return "", "", ""
	}
	for _, entry := range feed.Entries {
		if !youtubeVideoID.MatchString(entry.ID) || entry.Published.Before(updated.AddDate(0, 0, -14)) || entry.Published.After(updated.AddDate(0, 0, 14)) {
			continue
		}
		for _, candidate := range modVersionPattern.FindAllString(entry.Title, -1) {
			if candidate == version {
				return plain(entry.Title), plain(entry.Media.Description), "https://www.youtube.com/watch?v=" + entry.ID
			}
		}
	}
	return "", "", ""
}
