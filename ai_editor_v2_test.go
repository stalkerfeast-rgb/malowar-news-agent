package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDualEditorialOutputAndSafety(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			_ = json.NewEncoder(w).Encode(map[string]any{"models": []map[string]string{{"name": "qwen3:14b"}}})
			return
		}
		if r.URL.Path == "/api/ps" {
			_ = json.NewEncoder(w).Encode(map[string]any{"models": []any{}})
			return
		}
		if r.URL.Path != "/api/chat" {
			t.Errorf("wrong path %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["think"] != false || body["keep_alive"] != float64(0) || body["stream"] != false {
			t.Errorf("unsafe Ollama flags: %v", body)
		}
		if body["format"] == nil {
			t.Error("no JSON schema")
		}
		text := map[string]string{"short": "Stellaris: укрепления получили новые возможности\n\nОборонительные платформы получили обновление. Игроки могут размещать новые орудия на орбитальных станциях.\n\n#Stellaris #MalowarGaming #стратегии", "expanded": "Stellaris: новые возможности обороны\n\nВ обновлении описаны изменения космических крепостей и оборонительных платформ. Теперь на орбитальных станциях можно размещать тяжёлые орудия.\n\nЧто изменится в ваших оборонительных линиях?\n\n#Stellaris #MalowarGaming #стратегии"}
		content, _ := json.Marshal(text)
		_ = json.NewEncoder(w).Encode(map[string]any{"message": map[string]string{"content": string(content)}})
	}))
	defer server.Close()
	old := ollamaEndpoint
	ollamaEndpoint = server.URL
	defer func() { ollamaEndpoint = old }()
	a, err := newApp(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a.state.Config.AIProvider = "ollama"
	a.state.Config.AIModel = "qwen3:14b"
	a.state.Config.LocalAIEnabled = true
	a.state.Items = append(a.state.Items, &Item{ID: "test", Game: "Stellaris", Title: "Defence update", Category: "Обновления", Excerpt: "Defence platform changes: heavy weapons on orbital stations", Time: time.Now(), State: "new", URL: "https://example.com/news"})
	if err := a.rewriteItem(context.Background(), "test"); err != nil {
		t.Fatal(err)
	}
	it := a.state.Items[0]
	if it.AIOptions == nil || it.AIOptions.Short == "" || it.AIOptions.Expanded == "" {
		t.Fatal("variants not saved")
	}
	if it.Verified || it.State != "new" {
		t.Fatal("AI changed approval", it)
	}
	if err := a.chooseAIVariant("test", "expanded"); err != nil {
		t.Fatal(err)
	}
	if it.AIOptions.Selected != "expanded" || !strings.Contains(it.Draft, "новые возможности обороны") {
		t.Fatal("failed switch variant")
	}
	if !strings.Contains(it.Comment, "https://example.com/news") {
		t.Fatal("source should be in comments")
	}
}
func TestClaimWarnings(t *testing.T) {
	w := sourceWarning("Stellaris: новая оборона", "Stellaris: обновление вышло сегодня, 12.12.2026. Его уже можно скачать.")
	if len(w) < 2 {
		t.Fatal("expected claims warnings", w)
	}
}
func TestCannotSwitchApprovedPost(t *testing.T) {
	a, _ := newApp(t.TempDir())
	a.state.Items = append(a.state.Items, &Item{ID: "a", State: "approved", AIOptions: &AIVariants{Short: "one", Expanded: "two"}})
	if err := a.chooseAIVariant("a", "expanded"); err == nil {
		t.Fatal("approved modification allowed")
	}
}
