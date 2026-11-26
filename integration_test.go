package main

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type mockRoundTripper func(*http.Request) (*http.Response, error)

func (fn mockRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }
func fakeHTTP(b string) *http.Response {
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(b)), Header: http.Header{}}
}
func TestSteamParser(t *testing.T) {
	old := client
	defer func() { client = old }()
	client = &http.Client{Transport: mockRoundTripper(func(r *http.Request) (*http.Response, error) {
		return fakeHTTP(`{"appnews":{"newsitems":[{"gid":"55","title":"New DLC","url":"https://store.steampowered.com/news/","date":1795500000,"contents":"<b>Big</b> changes"}]}}`), nil
	})}
	items, err := fetchSteam(context.Background(), SteamSource{281990, "Stellaris"})
	if err != nil || len(items) != 1 || items[0].Excerpt != "Big changes" || items[0].Category != "DLC" {
		t.Fatalf("got %#v %v", items, err)
	}
}
func TestWorkshopParser(t *testing.T) {
	old := client
	defer func() { client = old }()
	client = &http.Client{Transport: mockRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.Method != "POST" {
			t.Fatal("expected POST")
		}
		return fakeHTTP(`{"response":{"publishedfiledetails":[{"result":1,"title":"Cool Mod","time_updated":1795500000,"file_description":"<p>New story</p>"}]}}`), nil
	})}
	items, err := fetchWorkshop(context.Background(), WorkshopSource{FileID: "123456789", Name: "Mod"})
	if err != nil || len(items) != 1 || items[0].Category != "Моды" {
		t.Fatalf("got %#v %v", items, err)
	}
}
func TestApprovalGuard(t *testing.T) {
	a, err := newApp(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	it := Item{ID: "x", State: "new", Game: "Game", Title: "News", URL: "https://example.com", Time: time.Now()}
	it.Draft = draftText(it)
	a.state.Items = append(a.state.Items, &it)
	if err := a.updateItem("x", it.Draft, "", "approved", true); err == nil {
		t.Fatal("template must not be approved")
	}
	if err := a.updateItem("x", "Game: официальный анонс. Все факты проверены по первоисточнику, детали и точная дата подтверждены.", "", "approved", true); err == nil {
		t.Fatal("approve must require 3 images")
	}
	a.state.Items[0].Images = []string{"one.jpg", "two.jpg", "three.jpg"}
	a.state.Config.VKBrowserMode = false
	a.state.Config.VKPostponedOnApproval = false
	if err := a.updateItem("x", "Game: официальный анонс. Все факты проверены по первоисточнику, детали и точная дата подтверждены.", "", "approved", true); err == nil {
		t.Fatal("approval without required hashtags must be blocked")
	}
	if err := a.updateItem("x", "Game: официальный анонс. Все факты проверены по первоисточнику, детали и точная дата подтверждены.\n\n#Game #Стратегии #Steam #MalowarGaming", "", "approved", true); err != nil {
		t.Fatal(err)
	}
	a2, err := newApp(a.dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if a2.state.Items[0].State != "approved" {
		t.Fatal("lost approval state")
	}
}
