package main

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestOfficialAnnouncementImagesOnly(t *testing.T) {
	input := `[img]https://clan.cloudflare.steamstatic.com/images/123/romania.jpg[/img]` +
		` https://notsteam.example/romania.jpg` +
		` https://shared.akamai.steamstatic.com/store_item_assets/dlc_art.png` +
		` https://evil.steamstatic.com.bad.test/payload.png`
	pics := officialImagesFromNews(input)
	if len(pics) != 2 || !strings.Contains(pics[0], "romania.jpg") {
		t.Fatalf("official images = %v", pics)
	}
}

func TestMediaPlanRomaniaNotBaseFirst(t *testing.T) {
	old := client
	defer func() { client = old }()
	client = &http.Client{Transport: mockRoundTripper(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Host {
		case "api.steampowered.com":
			if r.URL.Query().Get("appid") != "394360" {
				t.Fatalf("wrong app: %s", r.URL)
			}
			return fakeHTTP(`{"appnews":{"newsitems":[{"gid":"romanian-diary","contents":"[img]https://clan.cloudflare.steamstatic.com/images/hoi4/romania.jpg[/img]"}]}}`), nil
		case "store.steampowered.com":
			return fakeHTTP(`{"394360":{"success":true,"data":{"header_image":"https://shared.akamai.steamstatic.com/steam/apps/394360/header.jpg","screenshots":[{"path_full":"https://shared.akamai.steamstatic.com/steam/apps/394360/base.jpg"}]}}}`), nil
		}
		t.Fatalf("unexpected request %s", r.URL)
		return nil, nil
	})}
	a := &app{state: saved{Config: defaultConfig()}}
	item := Item{ID: "steam:394360:romanian-diary", Source: "Steam News", Game: "Hearts of Iron IV", Title: "Dev Diary: Romania"}
	choices, focus, err := a.mediaPlan(context.Background(), item)
	if err != nil || len(choices) < 2 {
		t.Fatalf("%v %v", choices, err)
	}
	if !strings.Contains(choices[0].URL, "romania.jpg") || !strings.Contains(choices[0].Label, "объявления") {
		t.Fatalf("unrelated art first: %v", choices)
	}
	if !strings.Contains(choices[len(choices)-1].Label, "Fallback") {
		t.Fatalf("fallback not declared: %v", choices)
	}
	if !strings.Contains(focus, "объявления") {
		t.Fatalf("focus %s", focus)
	}
}

func TestVerifiedDLCBeforeBase(t *testing.T) {
	old := client
	defer func() { client = old }()
	client = &http.Client{Transport: mockRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == "api.steampowered.com" {
			return fakeHTTP(`{"appnews":{"newsitems":[{"gid":"dlc","contents":"DLC: https://store.steampowered.com/app/5000/My_DLC/ [img]https://clan.cloudflare.steamstatic.com/images/123/dlc_diary.png[/img]"}]}}`), nil
		}
		if r.URL.Query().Get("appids") == "5000" && r.URL.Query().Get("l") != "" {
			return fakeHTTP(`{"5000":{"success":true,"data":{"type":"dlc","fullgame":{"appid":394360},"header_image":"https://shared.akamai.steamstatic.com/steam/apps/5000/header.jpg"}}}`), nil
		}
		if r.URL.Query().Get("appids") == "394360" {
			return fakeHTTP(`{"394360":{"success":true,"data":{"header_image":"https://shared.akamai.steamstatic.com/steam/apps/394360/header.jpg"}}}`), nil
		}
		t.Fatalf("unexpected request %s", r.URL)
		return nil, nil
	})}
	a := &app{state: saved{Config: defaultConfig()}}
	choices, _, err := a.mediaPlan(context.Background(), Item{ID: "steam:394360:dlc", Source: "Steam News", Title: "New DLC: Country Pack"})
	if err != nil || len(choices) < 3 {
		t.Fatalf("%v %v", choices, err)
	}
	if !strings.Contains(choices[0].URL, "/5000/library_hero") || !strings.Contains(choices[0].Label, "DLC") {
		t.Fatalf("DLC not first: %+v", choices)
	}
	if !strings.Contains(choices[len(choices)-1].Label, "Fallback") {
		t.Fatalf("unmarked base image: %+v", choices)
	}
}

func TestNoFakeRelatedDLC(t *testing.T) {
	old := client
	defer func() { client = old }()
	client = &http.Client{Transport: mockRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == "api.steampowered.com" {
			return fakeHTTP(`{"appnews":{"newsitems":[{"gid":"dlc","contents":"https://store.steampowered.com/app/5000/"}]}}`), nil
		}
		if r.URL.Query().Get("appids") == "5000" {
			return fakeHTTP(`{"5000":{"success":true,"data":{"type":"dlc","fullgame":{"appid":281990}}}}`), nil
		}
		return fakeHTTP(`{"394360":{"success":true,"data":{"header_image":"https://shared.akamai.steamstatic.com/steam/apps/394360/header.jpg"}}}`), nil
	})}
	a := &app{state: saved{Config: defaultConfig()}}
	choices, focus, err := a.mediaPlan(context.Background(), Item{ID: "steam:394360:dlc", Source: "Steam News", Title: "New DLC"})
	if err != nil || len(choices) == 0 {
		t.Fatal(err)
	}
	if !strings.Contains(choices[0].Label, "Fallback") || !strings.Contains(focus, "Тематические изображения не подтверждены") {
		t.Fatalf("unrelated DLC incorrectly used: %v %s", choices, focus)
	}
}

func TestSameGameRSSUsesExplicitFallback(t *testing.T) {
	old := client
	defer func() { client = old }()
	client = &http.Client{Transport: mockRoundTripper(func(r *http.Request) (*http.Response, error) {
		return fakeHTTP(`{"281990":{"success":true,"data":{"header_image":"https://shared.akamai.steamstatic.com/steam/apps/281990/header.jpg"}}}`), nil
	})}
	a := &app{state: saved{Config: defaultConfig()}}
	choices, focus, err := a.mediaPlan(context.Background(), Item{Source: "RSS", Game: "Stellaris", Title: "New diplomacy diary"})
	if err != nil || len(choices) == 0 || !strings.Contains(choices[0].Label, "Fallback") || !strings.Contains(focus, "fallback") {
		t.Fatalf("%v %s %v", choices, focus, err)
	}
}

func TestOrderMaintainsMediaProvenance(t *testing.T) {
	a, err := newApp(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a.state.Items = []*Item{{ID: "post", State: "new", Images: []string{"1.jpg", "2.jpg"}, MediaSources: []string{"a", "b"}, MediaLabels: []string{"Fallback: game", "Steam announcement"}}}
	if err := a.moveImage("post", 1, 0); err != nil {
		t.Fatal(err)
	}
	it := a.state.Items[0]
	if it.Images[0] != "2.jpg" || it.MediaSources[0] != "b" || it.MediaLabels[0] != "Steam announcement" {
		t.Fatalf("misaligned: %+v", it)
	}
}
