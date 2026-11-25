package main

import (
	"strings"
	"testing"
	"time"
)

func TestEditorialHeadlineAvoidsDoubling(t *testing.T) {
	cases := []struct{ game, title, want string }{
		{"Stellaris", "Stellaris: новые крепости", "Stellaris: новые крепости"},
		{"Stellaris", "Stellaris выйдет с DLC", "Stellaris выйдет с DLC"},
		{"Stellaris", "новые крепости", "Stellaris: новые крепости"},
		{"Age of Wonders 4", "Age of Wonders 4 — морское обновление", "Age of Wonders 4 — морское обновление"},
		{"Stellaris", "Stellarisium станет доступна", "Stellaris: Stellarisium станет доступна"},
	}
	for _, c := range cases {
		if got := editorialHeadline(c.game, c.title); got != c.want {
			t.Errorf("%q: got %q want %q", c.title, got, c.want)
		}
	}
}

func TestEditorialPolishOnlyVacuousOpening(t *testing.T) {
	text := "Stellaris: новое обновление\n\nStellaris — это космическая стратегия. В обновлении переработали орбитальные платформы, добавили дополнительные орудия и изменили защиту приграничных систем.\n\n#Stellaris #Стратегии"
	got := polishEditorialText(text, "Stellaris")
	if strings.Contains(got, "это космическая стратегия") || !strings.Contains(got, "переработали орбитальные платформы") {
		t.Fatal(got)
	}
	factual := "Stellaris: новые платформы\n\nStellaris — это стратегия, в которой появились новые корабли. В обновлении появились новые станции и улучшения оборонительных платформ.\n\n#Stellaris"
	if got := polishEditorialText(factual, "Stellaris"); got != factual {
		t.Fatalf("destroyed factual lead: %s", got)
	}
}

func TestEditorialWarningsCallOutThinAndDuplicate(t *testing.T) {
	generated := "Stellaris: новая оборона\n\nStellaris — это стратегия.\n\n#Stellaris"
	warnings := strings.Join(editorialQualityWarnings("Stellaris", strings.Repeat("Подробности из источника. ", 18), generated, "expanded"), "\n")
	if !strings.Contains(warnings, "повторяет") || !strings.Contains(warnings, "коротким") {
		t.Fatal(warnings)
	}
	scarce := strings.Join(editorialQualityWarnings("Stellaris", "Короткий анонс", generated, "expanded"), "\n")
	if !strings.Contains(scarce, "В источнике мало") {
		t.Fatal(scarce)
	}
}

func TestReleaseRadarDoesNotRepeatName(t *testing.T) {
	g := ReleaseGame{Name: "Stellaris", Description: "В игре переработали орбитальные станции и систему укреплений.", Date: "2026-11-24", DateText: "24 Nov 2026", ExactDate: true, ComingSoon: false, Released: true, Russian: "yes"}
	post := releasePost(g, false)
	if count := strings.Count(strings.Split(post.Draft, "#")[0], "Stellaris"); count != 1 {
		t.Fatalf("name repeated %d times: %q", count, post.Draft)
	}
	if !strings.Contains(post.Draft, "орбитальные станции") {
		t.Fatal(post.Draft)
	}
	preview := releasePost(g, true)
	if count := strings.Count(strings.Split(preview.Draft, "#")[0], "Stellaris"); count != 1 {
		t.Fatalf("preview repeated %d times: %q", count, preview.Draft)
	}
	item := Item{Game: "Stellaris", Title: "Stellaris: дополнение", Time: time.Now()}
	if !strings.HasPrefix(draftText(item), "Stellaris: дополнение\n") {
		t.Fatal(draftText(item))
	}
}
