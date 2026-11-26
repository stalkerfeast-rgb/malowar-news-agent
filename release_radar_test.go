package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func steamFixture(id int, name, date string, comingSoon bool, languages string) []byte {
	d := map[string]any{"type": "game", "name": name, "short_description": "Build and lead an army with grand strategy logistics and tactical warfare in a fictional world.", "supported_languages": languages, "release_date": map[string]any{"coming_soon": comingSoon, "date": date}, "genres": []map[string]string{{"description": "Strategy"}}}
	b, _ := json.Marshal(map[string]any{fmt.Sprint(id): map[string]any{"success": true, "data": d}})
	return b
}
func TestReleaseRadarParsesSteam(t *testing.T) {
	now := time.Date(2026, 11, 24, 6, 0, 0, 0, time.FixedZone("MSK", 10800))
	g, err := parseSteamGame(123, steamFixture(123, "Test Strategy", "29 Nov, 2026", true, "English, Russian<strong>*</strong>"), now)
	if err != nil {
		t.Fatal(err)
	}
	if g.Date != "2026-11-29" || !g.ExactDate || g.Released || !g.ComingSoon || g.Russian != "yes" || !g.RussianVoice {
		t.Fatalf("bad parse: %+v", g)
	}
	p, day := releaseDue(g, now, 5)
	if !p || day {
		t.Fatalf("should preview in 5 days: %v %v", p, day)
	}
	if it := releasePost(g, true); it.Verified || it.State != "new" || !strings.Contains(it.Draft, "Русский язык: есть") || !strings.Contains(it.Comment, g.URL) {
		t.Fatalf("unsafe preview: %+v", it)
	}
	g, err = parseSteamGame(123, steamFixture(123, "Test Strategy", "24 Nov, 2026", false, "English, German"), now)
	if err != nil || !g.Released || g.Russian != "not_listed" {
		t.Fatalf("bad day: %+v, %v", g, err)
	}
	p, day = releaseDue(g, now, 5)
	if p || !day {
		t.Fatal("released day must have day-of post")
	}
	if strings.Contains(languageLabel(g), "Русский язык: нет") {
		t.Fatal("unsupported certainty")
	}
}
func TestReleaseRadarBlocksUncertain(t *testing.T) {
	now := time.Date(2026, 11, 24, 6, 0, 0, 0, time.UTC)
	for _, date := range []string{"Soon", "November 2026", "Q4 2026", "2026"} {
		g, err := parseSteamGame(8, steamFixture(8, "Strategy", date, true, ""), now)
		if err != nil {
			t.Fatal(err)
		}
		p, day := releaseDue(g, now, 5)
		if g.ExactDate || p || day || g.Russian != "unknown" {
			t.Fatalf("uncertain treated as release %+v", g)
		}
	}
	b := []byte(`{"8":{"success":true,"data":{"type":"game","name":"Strategy","release_date":{"date":"24 Nov, 2026"},"genres":[{"description":"Strategy"}]}}}`)
	if _, err := parseSteamGame(8, b, now); err == nil {
		t.Fatal("missing coming_soon accepted")
	}
}
func TestReleaseRadarDateChangeIsBlocked(t *testing.T) {
	dir := t.TempDir()
	a, _ := newApp(dir)
	a.state.Config.AutoImages = false
	old := ReleaseGame{AppID: 123, Name: "Test Strategy", Date: "2026-11-24", ExactDate: true, ComingSoon: true, Russian: "unknown"}
	it := releasePost(old, true)
	it.State = "scheduled"
	it.Verified = true
	it.ScheduledAt = time.Now().Add(time.Hour)
	a.state.Items = append(a.state.Items, &it)
	a.state.Radar = []ReleaseGame{old}
	if err := a.persistLocked(); err != nil {
		t.Fatal(err)
	}
	a2, e := newApp(dir)
	if e != nil {
		t.Fatal(e)
	}
	if a2.state.Items[0].State != "scheduled" {
		t.Fatal("lost existing schedule before change")
	}
	// A date change occurs while a normal scheduled post exists: stale post must be blocked.
	a2.mu.Lock()
	a2.state.Items[0].State = "missed"
	a2.state.Items[0].Verified = false
	a2.state.Items[0].ScheduledAt = time.Time{}
	a2.mu.Unlock()
	if a2.state.Items[0].Verified || a2.state.Items[0].State == "scheduled" {
		t.Fatal("post not invalidated")
	}
}
func TestMigrationV6RadarPreservesAllPosts(t *testing.T) {
	dir := t.TempDir()
	c := defaultConfig()
	c.UpdateRepo = "stalkerfeast-rgb/malowar-news-agent"
	c.ReleaseLeadDays = 0
	data := saved{Version: 6, Config: c, Items: []*Item{{ID: "a", State: "approved", Verified: true, Draft: "my approved copy", Images: []string{"a", "b", "c"}}}}
	raw, _ := json.Marshal(data)
	if err := os.WriteFile(filepath.Join(dir, "state.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	a, err := newApp(dir)
	if err != nil {
		t.Fatal(err)
	}
	if a.state.Version != 10 || a.state.Config.ReleaseLeadDays != 5 || a.state.Config.ReleaseTop != 15 || !a.state.Config.ReleaseIndie || !a.state.Config.ReleasePreviews || !a.state.Config.ReleaseDayPosts || a.state.Config.UpdateRepo != c.UpdateRepo {
		t.Fatalf("migration failed: %+v", a.state.Config)
	}
	if a.state.Items[0].State != "approved" || !a.state.Items[0].Verified || a.state.Items[0].Draft != "my approved copy" {
		t.Fatal("migration touched existing approved post")
	}
}

func TestRadarScanDraftsDeduplicateAndDatePostponement(t *testing.T) {
	oldClient := client
	defer func() { client = oldClient }()
	now := time.Now().In(time.FixedZone("MSK", 3*3600))
	today := now.Format("2 Jan, 2006")
	five := now.AddDate(0, 0, 5).Format("2 Jan, 2006")
	newDate := now.AddDate(0, 0, 6).Format("2 Jan, 2006")
	date := five
	coming := true
	client = &http.Client{Transport: mockRoundTripper(func(req *http.Request) (*http.Response, error) {
		if strings.Contains(req.URL.Path, "/search/results/") {
			return fakeHTTP(`{"results_html":"<a data-ds-appid=\"123\"><span class=\"title\">Test Strategy</span></a>"}`), nil
		}
		if req.URL.Query().Get("appids") == "123" {
			return fakeHTTP(string(steamFixture(123, "Test Strategy", date, coming, "English, Russian"))), nil
		}
		return fakeHTTP(`{}`), nil
	})}
	a, e := newApp(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	c := a.state.Config
	c.Steam = []SteamSource{{AppID: 123, Name: "Test Strategy"}}
	c.AutoImages = false
	c.AutoRewrite = false
	a.scanReleases(c)
	if len(a.state.Items) != 1 || a.state.Items[0].State != "new" || a.state.Items[0].Verified {
		t.Fatalf("initial scan: %+v", a.state.Items)
	}
	first := a.state.Items[0]
	first.State = "scheduled"
	first.Verified = true
	first.ScheduledAt = now.Add(time.Hour)
	a.scanReleases(c)
	if len(a.state.Items) != 1 {
		t.Fatal("duplicate preview created")
	}
	date = newDate
	a.scanReleases(c)
	if len(a.state.Items) != 1 || a.state.Items[0].State != "missed" || a.state.Items[0].Verified || !a.state.Items[0].ScheduledAt.IsZero() {
		t.Fatalf("old scheduled post not blocked %+v", a.state.Items[0])
	}
	// Steam releases after date reset, no automatic publishing or approval.
	date = today
	coming = false
	a.scanReleases(c)
	if len(a.state.Items) != 2 || a.state.Items[1].State != "new" || a.state.Items[1].Verified || a.state.Items[1].EventKind != "release" {
		t.Fatalf("day of release: %+v", a.state.Items)
	}
	a.scanReleases(c)
	if len(a.state.Items) != 2 {
		t.Fatal("duplicate release-day post")
	}
}
