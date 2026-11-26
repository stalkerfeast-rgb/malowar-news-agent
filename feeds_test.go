package main

import (
	"testing"
	"time"
)

func TestPlain(t *testing.T) {
	got := plain("<b>Hello</b> [url=https://x]War[/url] &amp; Mars")
	if got != "Hello War & Mars" {
		t.Fatalf("got %q", got)
	}
}
func TestClassify(t *testing.T) {
	if classify("Patch 1.2", "Steam News") != "Обновления" {
		t.Fatal("category")
	}
}
func TestDate(t *testing.T) {
	if parseDate("Tue, 24 Nov 2026 10:00:00 +0000").Before(time.Date(2026, 11, 24, 9, 59, 0, 0, time.UTC)) {
		t.Fatal("date")
	}
}
