package main

import (
	"testing"
	"time"
)

func TestMatchWorkshopVideoRequiresExactVersionAndRecentDate(t *testing.T) {
	feed := []byte(`<feed xmlns:yt="urn:youtube" xmlns:media="urn:media">
	<entry><yt:videoId>aaaaaaaaaaa</yt:videoId><title>Build 0.5.2.10</title><published>2026-11-24T00:00:00Z</published></entry>
	<entry><yt:videoId>bbbbbbbbbbb</yt:videoId><title>Build 0.5.2.1</title><published>2025-11-24T00:00:00Z</published></entry>
	<entry><yt:videoId>ccccccccccc</yt:videoId><title>Build 0.5.2.1</title><published>2026-11-25T00:00:00Z</published><media:group><media:description>Patch details</media:description></media:group></entry>
	</feed>`)
	updated := time.Date(2026, 11, 25, 0, 0, 0, 0, time.UTC)
	title, description, link := matchWorkshopVideo(feed, "0.5.2.1", updated)
	if title != "Build 0.5.2.1" || description != "Patch details" || link != "https://www.youtube.com/watch?v=ccccccccccc" {
		t.Fatalf("wrong video evidence: %q %q %q", title, description, link)
	}
}

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
