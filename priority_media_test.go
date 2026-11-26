package main

import (
	"context"
	"net/http"
	"testing"
)

func TestSteamImageSourceValidation(t *testing.T) {
	for _, x := range []string{"https://cdn.cloudflare.steamstatic.com/steam/apps/10/foo.jpg", "https://shared.akamai.steamstatic.com/store_item_assets/x.jpg", "https://images.steamusercontent.com/ugc/foo.jpg"} {
		if !trustedSteamImage(x) {
			t.Errorf("expected allow %s", x)
		}
	}
	for _, x := range []string{"http://cdn.cloudflare.steamstatic.com/steam/apps/10/foo.jpg", "https://steamstatic.com.evil.example/x.jpg", "https://example.org/x.jpg", "file:///etc/passwd", "https://evil.com@evil.net/a.png"} {
		if trustedSteamImage(x) {
			t.Errorf("expected reject %s", x)
		}
	}
}
func TestSteamImageCatalog(t *testing.T) {
	old := client
	defer func() { client = old }()
	client = &http.Client{Transport: mockRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.URL.Query().Get("appids") != "281990" {
			t.Fatal("wrong steam app id")
		}
		return fakeHTTP(`{"281990":{"success":true,"data":{"screenshots":[{"path_full":"https://shared.akamai.steamstatic.com/ss_1.jpg"},{"path_full":"https://example.com/bad.jpg"},{"path_full":"https://shared.akamai.steamstatic.com/ss_2.jpg"}]}}}`), nil
	})}
	urls, err := collectSteamImages(context.Background(), 281990)
	if err != nil || len(urls) != 3 || urls[0] != "https://cdn.akamai.steamstatic.com/steam/apps/281990/library_hero.jpg" {
		t.Fatalf("got %v %v", urls, err)
	}
}
func TestPriorityHeuristics(t *testing.T) {
	good := Item{Game: "Total War", Category: "Релизы", Title: "Major expansion release announcement"}
	weak := Item{Game: "Some Mod", Category: "Обновления", Title: "Minor hotfix"}
	if scoreNews(good) <= scoreNews(weak) {
		t.Fatalf("priority broken %d <= %d", scoreNews(good), scoreNews(weak))
	}
}
