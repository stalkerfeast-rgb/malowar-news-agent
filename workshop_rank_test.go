package main

import (
	"encoding/json"
	"testing"
	"time"
)

func TestParseWorkshopRankingReadsSteamLoaderAndFiltersCollections(t *testing.T) {
	loader := `{"results":[
		{"publishedfileid":"1","consumer_appid":394360,"file_type":0,"title":"First","time_updated":100},
		{"publishedfileid":"2","consumer_appid":394360,"file_type":2,"title":"Collection"},
		{"publishedfileid":"1","consumer_appid":394360,"file_type":0,"title":"Duplicate"},
		{"publishedfileid":"3","consumer_appid":1,"file_type":0,"title":"Wrong game"},
		{"publishedfileid":"4","consumer_appid":394360,"file_type":0,"title":"Second"}
	]}`
	payload, err := json.Marshal(map[string]any{"renderContext": map[string]string{"queryData": loader}})
	if err != nil {
		t.Fatal(err)
	}
	page := []byte(`<script type="application/json" id="valve-ssr-data">` + string(payload) + `</script>`)
	items := parseWorkshopRanking(page, 394360)
	if len(items) != 2 || items[0].ID != "1" || items[0].Updated != 100 || items[1].ID != "4" {
		t.Fatalf("unexpected ranking: %+v", items)
	}
}

func TestRankedWorkshopUpdatesNeedsRecentUnseenPatch(t *testing.T) {
	now := time.Date(2026, 11, 26, 10, 0, 0, 0, time.UTC)
	game := rankedWorkshopGames[0]
	recent := now.Add(-time.Hour).Unix()
	ranks := map[int]workshopRanking{
		game.AppID: {Items: []workshopRankItem{
			{ID: "1", AppID: game.AppID, Updated: recent},
			{ID: "2", AppID: game.AppID, Updated: recent},
			{ID: "3", AppID: game.AppID, Updated: recent},
			{ID: "4", AppID: game.AppID, Updated: now.Add(-96 * time.Hour).Unix(), NewInTop: true},
			{ID: "5", AppID: game.AppID, Updated: now.Add(time.Hour).Unix()},
			{ID: "6", AppID: game.AppID, Updated: recent, Type: 2},
		}},
	}
	updates := rankedWorkshopUpdates(ranks, map[string]bool{workshopNewsID("2", recent): true}, map[string]bool{"3": true}, now)
	if len(updates) != 1 || updates[0].mod.ID != "1" || updates[0].game != game.Name {
		t.Fatalf("unexpected updates: %+v", updates)
	}
}
