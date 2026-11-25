package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"time"
)

var rankedWorkshopGames = []SteamSource{
	{394360, "Hearts of Iron IV"},
	{236850, "Europa Universalis IV"},
	{1158310, "Crusader Kings III"},
	{281990, "Stellaris"},
	{529340, "Victoria 3"},
	{1142710, "Total War: Warhammer III"},
}

type workshopRankItem struct {
	ID           string `json:"publishedfileid"`
	AppID        int    `json:"consumer_appid"`
	Type         int    `json:"file_type"`
	Title        string `json:"title"`
	Updated      int64  `json:"time_updated"`
	PreviousRank int    `json:"previous_rank,omitempty"`
	NewInTop     bool   `json:"new_in_top,omitempty"`
	JustUpdated  bool   `json:"just_updated,omitempty"`
}

type workshopRanking struct {
	SteamSource
	Checked time.Time          `json:"checked"`
	Items   []workshopRankItem `json:"items"`
}

var workshopResults = regexp.MustCompile(`"results"\s*:\s*\[`)
var workshopSSR = regexp.MustCompile(`(?is)<script\b[^>]*\bid="valve-ssr-data"[^>]*>(.*?)</script>`)

func parseWorkshopRanking(body []byte, appID int) []workshopRankItem {
	if match := workshopSSR.FindSubmatch(body); len(match) == 2 {
		var page struct {
			LoaderData []string `json:"loaderData"`
		}
		if json.Unmarshal(match[1], &page) == nil {
			for _, data := range page.LoaderData {
				if items := decodeWorkshopRanking([]byte(data), appID); len(items) != 0 {
					return items
				}
			}
		}
	}
	return decodeWorkshopRanking(body, appID)
}

func decodeWorkshopRanking(body []byte, appID int) []workshopRankItem {
	for _, match := range workshopResults.FindAllIndex(body, -1) {
		var items []workshopRankItem
		if err := json.NewDecoder(bytes.NewReader(body[match[1]-1:])).Decode(&items); err != nil {
			continue
		}
		out := make([]workshopRankItem, 0, 30)
		seen := map[string]bool{}
		for _, item := range items {
			id, err := strconv.ParseUint(item.ID, 10, 64)
			if item.AppID != appID || item.Type != 0 || item.Title == "" || err != nil || id == 0 || seen[item.ID] {
				continue
			}
			seen[item.ID] = true
			out = append(out, item)
			if len(out) == 30 {
				break
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	return nil
}

func scanWorkshopRankings(ctx context.Context, previous map[int]workshopRanking, now time.Time) map[int]workshopRanking {
	results := make(map[int]workshopRanking, len(rankedWorkshopGames))
	updates := make(chan workshopRanking, len(rankedWorkshopGames))
	pending := 0
	for _, game := range rankedWorkshopGames {
		old, exists := previous[game.AppID]
		if exists {
			results[game.AppID] = old
			if now.Sub(old.Checked) < 6*time.Hour {
				continue
			}
		}
		pending++
		go func(game SteamSource) {
			url := fmt.Sprintf("https://steamcommunity.com/workshop/browse/?appid=%d&browsesort=trend&actualsort=trend&days=7&numperpage=30&section=items&l=english", game.AppID)
			body, err := get(ctx, url, nil)
			ranking := workshopRanking{SteamSource: game, Checked: now}
			if err == nil {
				ranking.Items = parseWorkshopRanking(body, game.AppID)
			}
			updates <- ranking
		}(game)
	}
	for i := 0; i < pending; i++ {
		ranking := <-updates
		if len(ranking.Items) > 0 {
			if old, exists := previous[ranking.AppID]; exists {
				positions := make(map[string]int, len(old.Items))
				for i, mod := range old.Items {
					positions[mod.ID] = i + 1
				}
				for i := range ranking.Items {
					mod := &ranking.Items[i]
					mod.PreviousRank = positions[mod.ID]
					mod.NewInTop = mod.PreviousRank == 0
					mod.JustUpdated = mod.PreviousRank > 0 && mod.Updated > old.Items[mod.PreviousRank-1].Updated
				}
			}
			results[ranking.AppID] = ranking
		}
	}
	return results
}
