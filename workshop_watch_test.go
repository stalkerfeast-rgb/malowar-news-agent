package main

import "testing"

func TestExtractWorkshopChangeMatchesUpdateTimestamp(t *testing.T) {
	body := []byte(`<p id="102">Current patch<br>Fixes A &amp; B</p><p id="101">Older patch</p>`)
	if got := extractWorkshopChange(body, 102); got != "Current patch Fixes A & B" {
		t.Fatalf("unexpected notes: %q", got)
	}
	if got := extractWorkshopChange(body, 103); got != "" {
		t.Fatalf("stale patch notes used for a different update: %q", got)
	}
}

func TestFeaturedWorkshopPreservesConfiguredSource(t *testing.T) {
	custom := WorkshopSource{FileID: "2962333032", Name: "Моя подпись", ParentGame: "Crusader Kings III"}
	sources := workshopSources([]WorkshopSource{custom})
	if len(sources) != 2 || sources[0] != custom || sources[1].FileID != "2887120253" {
		t.Fatalf("wrong Workshop list: %+v", sources)
	}
}
