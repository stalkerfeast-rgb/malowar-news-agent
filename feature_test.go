package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLinksInCommentsOnly(t *testing.T) {
	draft, comment := separateLinks("Событие важное. https://example.org/game?x=1!\n#стратегии", "Источник: https://store.steampowered.com/app/123", "https://example.org/news")
	if strings.Contains(draft, "http") {
		t.Fatal("link remains in post", draft)
	}
	for _, u := range []string{"https://example.org/game?x=1", "https://store.steampowered.com/app/123", "https://example.org/news"} {
		if !strings.Contains(comment, u) {
			t.Fatalf("missing %s: %q", u, comment)
		}
	}
}
func TestVKSourceComment(t *testing.T) {
	old := client
	defer func() { client = old }()
	client = &http.Client{Transport: mockRoundTripper(func(r *http.Request) (*http.Response, error) {
		if !strings.Contains(r.URL.Path, "wall.createComment") {
			t.Fatalf("wrong method %s", r.URL.Path)
		}
		b, _ := io.ReadAll(r.Body)
		payload := string(b)
		for _, term := range []string{"post_id=33", "owner_id=-123", "message="} {
			if !strings.Contains(payload, term) {
				t.Fatalf("missing %s in %s", term, payload)
			}
		}
		return fakeHTTP(`{"response":{"comment_id":44}}`), nil
	})}
	id, err := sendSourceComment(context.Background(), "fake-token", "123", 33, "https://example.org", "item-1")
	if err != nil || id != 44 {
		t.Fatalf("comment %d err %v", id, err)
	}
}
func TestMigrationToComments(t *testing.T) {
	dir := t.TempDir()
	x := saved{Version: 1, Config: defaultConfig(), Items: []*Item{{ID: "old", Game: "X", State: "new", Title: "News", URL: "https://example.org/news", Draft: "X. https://example.org/news", Time: time.Now()}}}
	b, _ := json.Marshal(x)
	if err := os.WriteFile(filepath.Join(dir, "state.json"), b, 0600); err != nil {
		t.Fatal(err)
	}
	a, err := newApp(dir)
	if err != nil {
		t.Fatal(err)
	}
	if a.state.Version != 10 || !strings.Contains(a.state.Items[0].Comment, "https://example.org/news") {
		t.Fatalf("bad migration: %+v", a.state)
	}
}
