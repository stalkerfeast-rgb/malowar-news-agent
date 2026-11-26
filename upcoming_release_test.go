package main

import (
	"strings"
	"testing"
	"time"
)

func TestUpcomingDraftCanBePreparedBeforeSteamAnnouncesDate(t *testing.T) {
	a, err := newApp(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a.state.Config.AutoImages = false
	a.state.Config.AutoRewrite = false
	game := ReleaseGame{AppID: 4199910, Name: "Total War: WARHAMMER 40,000", GenreStatus: "strategy", ComingSoon: true}
	a.state.Radar = []ReleaseGame{game}
	now := time.Date(2026, 11, 26, 9, 0, 0, 0, time.UTC)
	id, err := a.prepareUpcomingDraft(game.AppID, now)
	if err != nil {
		t.Fatal(err)
	}
	repeatedID, err := a.prepareUpcomingDraft(game.AppID, now)
	if err != nil || id != repeatedID || len(a.state.Items) != 1 {
		t.Fatalf("request duplicated a draft: %q %q %v", id, repeatedID, err)
	}
	item := a.state.Items[0]
	if item.State != "new" || item.Verified || !item.ScheduledAt.IsZero() || !strings.Contains(item.Draft, "Точная дата выхода пока не объявлена") {
		t.Fatalf("wrong upcoming draft: %+v", item)
	}
	a.state.Radar[0].ComingSoon = false
	if _, err := a.prepareUpcomingDraft(game.AppID, now); err == nil {
		t.Fatal("allowed an advance draft for a game that is no longer upcoming")
	}
}
