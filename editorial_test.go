package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestEditorialGameGroups(t *testing.T) {
	c := defaultConfig()
	c.IndieGames = []string{"Tiny Strategy"}
	cases := []struct{ name, game, bucket string }{
		{"paradox", "Stellaris", bucketParadox},
		{"paradox edition", "Europa Universalis V", bucketParadox},
		{"major", "Age of Wonders 4", bucketMajor},
		{"major franchise", "Total War: Pharaoh", bucketMajor},
		{"indie confirmed", "Tiny Strategy", bucketIndie},
		{"unknown not falsely asserted indie", "New RTS Unknown", bucketCandidate},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := editorialBucket(Item{Game: tt.game}, c)
			if got != tt.bucket {
				t.Fatalf("%q got %s want %s", tt.game, got, tt.bucket)
			}
		})
	}
}
func TestModAlsoAppearsInParadoxAndIsSingleItem(t *testing.T) {
	a, e := newApp(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	original := &Item{ID: "mod:1", Game: "Example Mod", ParentGame: "Stellaris", Title: "Update", Category: "Моды", Source: "Workshop", State: "new", Time: time.Now()}
	a.state.Items = []*Item{original}
	x := a.snapshot()["items"].([]*Item)
	if len(x) != 1 || x[0].EditorialBucket != bucketParadox || x[0].EventKind != "mod" {
		t.Fatalf("wrong multiple classification: %#v", x)
	}
	if err := a.setEditorialTag(original.ID, bucketIndie, "Tiny Strategy"); err != nil {
		t.Fatal(err)
	}
	again := a.snapshot()["items"].([]*Item)
	if len(again) != 1 || again[0].EditorialBucket != bucketIndie || again[0].EventKind != "mod" || again[0].State != "new" {
		t.Fatalf("override changed content or workflow: %#v", again)
	}
	restored, e := newApp(a.dataDir)
	if e != nil {
		t.Fatal(e)
	}
	if restored.state.Items[0].EditorialOverride != bucketIndie || restored.state.Items[0].ParentGame != "Tiny Strategy" {
		t.Fatal("saved editorial override lost")
	}
}
func TestEventTypes(t *testing.T) {
	cases := []struct{ title, category, source, want string }{
		{"Now in Early Access", "Релизы", "Steam News", "early_access"},
		{"Out now!", "Релизы", "Steam News", "release"},
		{"New expansion", "DLC", "Steam News", "dlc"},
		{"Hotfix #4", "Обновления", "Steam News", "update"},
		{"Hello world", "Моды", "Workshop", "mod"},
		{"Developer Diary", "Новости", "Steam News", "news"},
	}
	for _, tc := range cases {
		got := eventKind(Item{Title: tc.title, Category: tc.category, Source: tc.source})
		if got != tc.want {
			t.Fatalf("%+v got %s", tc, got)
		}
	}
}
func TestUpgradeV5KeepsEditorialDataAndApprovals(t *testing.T) {
	dir := t.TempDir()
	old := saved{Version: 5, Config: defaultConfig(), Items: []*Item{{ID: "x", Game: "Stellaris", Title: "Patch", State: "approved", Draft: "Existing user text", Comment: "Source: URL", Images: []string{"a", "b", "c"}, Verified: true}}}
	old.Config.CoreGames = nil // field did not exist in 1.3.5
	raw, e := json.Marshal(old)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(dir, "state.json"), raw, 0600); e != nil {
		t.Fatal(e)
	}
	a, e := newApp(dir)
	if e != nil {
		t.Fatal(e)
	}
	if a.state.Version != 10 || len(a.state.Config.CoreGames) == 0 {
		t.Fatal("editorial migration failed")
	}
	it := a.state.Items[0]
	if it.State != "approved" || !it.Verified || it.Draft != "Existing user text" || len(it.Images) != 3 {
		t.Fatalf("existing approval changed: %+v", it)
	}
}
func TestEditorialTagRejectsUnexpectedBucket(t *testing.T) {
	a, _ := newApp(t.TempDir())
	a.state.Items = []*Item{{ID: "x", State: "new"}}
	if err := a.setEditorialTag("x", "<script>", "Stellaris"); err == nil {
		t.Fatal("invalid tag accepted")
	}
}
