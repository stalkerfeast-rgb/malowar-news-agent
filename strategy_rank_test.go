package main

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestStrategyRankingUsesAverageAndCapsWatchlist(t *testing.T) {
	history := make(map[int][]strategySample)
	for _, game := range rankedStrategies {
		if _, duplicate := history[game.AppID]; duplicate {
			t.Fatalf("duplicate Steam AppID %d", game.AppID)
		}
		history[game.AppID] = []strategySample{{Players: 100}}
	}
	history[rankedStrategies[0].AppID] = []strategySample{{Players: 300}, {Players: 100}}
	top := topStrategies(history)
	if len(top) != 50 || top[0].AppID != rankedStrategies[0].AppID || top[0].Average != 200 || top[0].Samples != 2 {
		t.Fatalf("wrong top: %d entries, first %+v", len(top), top[0])
	}
}

func TestRankedSteamNewsAppearInMajorStrategyFeed(t *testing.T) {
	a, err := newApp(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a.state.StrategyRanks.Games = []strategyRank{{SteamSource: SteamSource{AppID: 916440, Name: "Anno 1800"}}}
	a.state.Items = []*Item{
		{ID: uid("steam", "916440", "1"), Source: "Steam News", Game: "Anno 1800"},
		{ID: uid("steam", "916440", "2"), Source: "Steam News", Game: "Anno 1800", EditorialOverride: bucketCandidate},
		{ID: uid("steam", "281990", "3"), Source: "Steam News", Game: "Stellaris"},
	}
	expected := map[string]string{a.state.Items[0].ID: bucketMajor, a.state.Items[1].ID: bucketCandidate, a.state.Items[2].ID: bucketParadox}
	for _, item := range a.snapshot()["items"].([]*Item) {
		if item.EditorialBucket != expected[item.ID] {
			t.Errorf("%q classified as %q, want %q", item.Game, item.EditorialBucket, expected[item.ID])
		}
	}
}

func TestStrategyRankingOnSteam(t *testing.T) {
	if os.Getenv("MNA_LIVE_STEAM_RANK_TEST") != "1" {
		t.Skip("requires live Steam player counts")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	ranking := refreshStrategyRanking(ctx, strategyRanking{}, time.Now().UTC())
	if len(ranking.Games) != 50 {
		t.Fatalf("received %d ranked games from Steam", len(ranking.Games))
	}
	for _, game := range ranking.Games[:5] {
		t.Logf("%s: %d", game.Name, game.Average)
	}
}
