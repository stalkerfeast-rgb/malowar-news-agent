package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestInterestRankingWeightsAndMissingValues(t *testing.T) {
	now := time.Date(2026, 11, 24, 15, 0, 0, 0, time.FixedZone("MSK", 3*3600))
	g := ReleaseGame{AppID: 42, Name: "RTS", GenreStatus: "strategy", Russian: "yes", PopularityRank: 1, PopularityPool: 100, ExactDate: true, Date: "2026-11-29"}
	scoreInterest(&g, now)
	if g.InterestScore == nil || *g.InterestScore != 99 || g.ScoreCoverage != 70 {
		t.Fatalf("expected weighted partial score 99 (70%% coverage), got %+v", g)
	}
	g.MomentumRank = 5
	g.MomentumKnown = true
	scoreInterest(&g, now)
	if g.InterestScore == nil || g.ScoreCoverage != 100 || *g.InterestScore <= 80 {
		t.Fatalf("growth not integrated: %+v", g)
	}
	g.PopularityRank = 0
	scoreInterest(&g, now)
	if g.InterestScore != nil {
		t.Fatal("no popularity source must not produce a score")
	}
	g.PopularityRank = 50
	g.GenreStatus = "review"
	scoreInterest(&g, now)
	if g.InterestScore != nil || len(g.AwardSuggestions) != 0 {
		t.Fatal("unapproved hybrid must not receive a score/award")
	}
}
func TestPopularityObservationsAreRanksNotPercent(t *testing.T) {
	now := time.Now().UTC()
	prev := ReleaseGame{PopularityRank: 40, PopularityPool: 90, PopularityObservedAt: now.Add(-time.Hour)}
	var newG ReleaseGame
	applyPopularityObservation(&newG, prev, 28, 90, now)
	if newG.MomentumRank != 12 || !newG.MomentumKnown || newG.PopularityRank != 28 {
		t.Fatalf("rank movement incorrect %+v", newG)
	}
}
func TestHybridRequiresExplicitReview(t *testing.T) {
	now := time.Now()
	data := map[string]any{"type": "game", "name": "Base Hybrid", "short_description": "Manage resources, plan production chains, and build your base while exploring.", "supported_languages": "English", "release_date": map[string]any{"coming_soon": true, "date": "30 Nov, 2026"}, "genres": []map[string]string{{"description": "Adventure"}}}
	raw, _ := json.Marshal(map[string]any{"543": map[string]any{"success": true, "data": data}})
	g, err := parseSteamGame(543, raw, now)
	if err != nil || g.GenreStatus != "review" || len(g.GenreEvidence) < 2 {
		t.Fatalf("hybrid candidate unexpectedly rejected/approved: %+v %v", g, err)
	}
	a, _ := newApp(t.TempDir())
	a.state.Radar = []ReleaseGame{g}
	if err := a.approveHybrid(543, true); err != nil {
		t.Fatal(err)
	}
	if a.state.Radar[0].GenreStatus != "hybrid" || len(a.state.Config.HybridApproved) != 1 {
		t.Fatal("hybrid approval not saved")
	}
	restored, err := newApp(a.dataDir)
	if err != nil || restored.state.Radar[0].GenreStatus != "hybrid" {
		t.Fatal("hybrid approval not preserved")
	}
	if err := restored.approveHybrid(543, false); err != nil {
		t.Fatal(err)
	}
	if restored.state.Radar[0].GenreStatus != "review" {
		t.Fatal("hybrid confirmation did not revoke")
	}
	_, err = parseSteamGame(543, steamFixture(543, "No strategic evidence", "30 Nov, 2026", true, "English"), now)
	if err != nil {
		t.Fatal("pure strategy should remain eligible")
	}
	data["short_description"] = "Run, jump and collect coins in this platform adventure"
	raw, _ = json.Marshal(map[string]any{"543": map[string]any{"success": true, "data": data}})
	_, err = parseSteamGame(543, raw, now)
	if err == nil {
		t.Fatal("non-strategy game wrongly accepted")
	}
}
func TestHashtagEnforcementAndMoscowToday(t *testing.T) {
	post := "Stellaris: вышло обновление\n\nСодержание обновления подтверждено."
	fixed := ensureMandatoryHashtags(post, "Новости")
	if !hasMandatoryHashtags(fixed) {
		t.Fatalf("tags not restored: %q", fixed)
	}
	if hasMandatoryHashtags(post) {
		t.Fatal("missing tags accepted")
	}
	if got := ensureMandatoryHashtags(fixed, "Новости"); got != fixed {
		t.Fatal("normalization is not idempotent")
	}
	now := time.Date(2026, 11, 24, 22, 30, 0, 0, time.UTC) // 25 Nov in Moscow
	g := ReleaseGame{AppID: 5, Name: "Test Strategy", Date: "2026-11-25", DateText: "25 Nov, 2026", ExactDate: true, ComingSoon: true}
	intro := releasePostAt(g, true, now)
	if !strings.Contains(intro.Title, "сегодня") || !hasMandatoryHashtags(intro.Draft) {
		t.Fatalf("must say today in Moscow: %s", intro.Title)
	}
	g.ComingSoon = false
	g.Released = true
	day := releasePostAt(g, false, now)
	if !strings.Contains(day.Title, "сегодня") {
		t.Fatalf("release title must say today: %s", day.Title)
	}
}
func TestUpgradeKeepsPreviouslyApprovedContent(t *testing.T) {
	dir := t.TempDir()
	c := defaultConfig()
	c.HybridApproved = []int{48}
	item := &Item{ID: "old", Game: "Stellaris", Draft: "User-edited content no tags", Images: []string{"one", "two", "three"}, State: "approved", Verified: true}
	old := saved{Version: 7, Config: c, Items: []*Item{item}}
	raw, _ := json.Marshal(old)
	if err := os.WriteFile(filepath.Join(dir, "state.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	a, err := newApp(dir)
	if err != nil {
		t.Fatal(err)
	}
	if a.state.Version != 10 || a.state.Items[0].Draft != item.Draft || !a.state.Items[0].Verified || len(a.state.Config.HybridApproved) != 1 {
		t.Fatalf("migration changed user data: %v", a.state)
	}
	if err := a.persistLocked(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "state.json")); err != nil {
		t.Fatal(err)
	}
	_ = fmt.Sprint(a)
}
