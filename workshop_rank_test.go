package main

import (
	"encoding/json"
	"testing"
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
