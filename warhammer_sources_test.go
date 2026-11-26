package main

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestWarhammerBookRequiresEvidenceOfRussianEdition(t *testing.T) {
	now := time.Date(2026, 11, 26, 10, 0, 0, 0, time.UTC)
	url := warhammerBookshop + "/catalog/warhammer_books/1/"
	if item := parseWarhammerBook([]byte("<article>Купить книгу. Доставка по России.</article>"), url, "Книга", now); item.ID != "" {
		t.Fatal("Russian storefront alone was treated as evidence of a Russian edition")
	}
	if item := parseWarhammerBook([]byte("<article>Перевод Антона Краснова</article>"), url, "Книга", now); item.ID == "" || item.Category != "Книги" {
		t.Fatalf("valid translated edition was skipped: %+v", item)
	}
}

func TestCollectLiveWarhammerSources(t *testing.T) {
	if os.Getenv("MNA_LIVE_WARHAMMER_TEST") != "1" {
		t.Skip("requires current Warhammer Community and retailer pages")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	items, ok := collectWarhammerNews(ctx, time.Now().UTC())
	counts := map[string]int{}
	for _, item := range items {
		counts[item.Category]++
		t.Logf("%s: %s", item.Category, item.Title)
	}
	if !ok || len(counts) < 3 {
		t.Fatalf("missing live source categories: %v", counts)
	}
}
