package main

import (
	"strings"
	"testing"
)

func TestDescriptionDiff(t *testing.T) {
	got := excerptDifference("Expanded kingdoms and armies", "Expanded kingdoms and armies with diplomacy system and ships")
	if !strings.Contains(got, "diplomacy") || !strings.Contains(got, "не официальный changelog") {
		t.Fatal(got)
	}
}
func TestVKPostMetrics(t *testing.T) {
	posts, e := decodeVKPosts([]byte(`[{"id":1,"views":{"count":420},"likes":{"count":12}}]`))
	if e != nil || len(posts) != 1 || posts[0].Views.Count == nil || *posts[0].Views.Count != 420 {
		t.Fatal(e, posts)
	}
}
