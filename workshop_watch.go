package main

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

const workshopItemURL = "https://steamcommunity.com/sharedfiles/filedetails/?id="
const workshopChangesURL = "https://steamcommunity.com/sharedfiles/filedetails/changelog/"

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
