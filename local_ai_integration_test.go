package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestLocalAISwitchChecksInstalledAndUnloads(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			_ = json.NewEncoder(w).Encode(map[string]any{"models": []map[string]string{{"name": "qwen3:14b"}}})
		case "/api/ps":
			_ = json.NewEncoder(w).Encode(map[string]any{"models": []map[string]string{{"name": "qwen3:14b"}}})
		case "/api/generate":
			var q map[string]any
			if err := json.NewDecoder(r.Body).Decode(&q); err != nil {
				t.Errorf("decode: %v", err)
			}
			if q["keep_alive"] != float64(0) || q["model"] != "qwen3:14b" {
				t.Errorf("unload params: %v", q)
			}
			mu.Lock()
			calls++
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]string{"response": ""})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	old := ollamaEndpoint
	ollamaEndpoint = srv.URL
	defer func() { ollamaEndpoint = old }()
	a, err := newApp(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	msg, err := a.setLocalAI(ctx, true)
	if err != nil || msg == "" {
		t.Fatalf("enable: %s %v", msg, err)
	}
	snap := a.snapshot()
	cfg := snap["config"].(Config)
	if !cfg.LocalAIEnabled || cfg.AIProvider != "ollama" || cfg.AIModel != "qwen3:14b" || cfg.AutoRewrite {
		t.Fatalf("unexpected enabled config: %+v", cfg)
	}
	mu.Lock()
	if calls != 0 {
		t.Fatal("enable unexpectedly loaded or unloaded model")
	}
	mu.Unlock()
	msg, err = a.setLocalAI(ctx, false)
	if err != nil || msg == "" {
		t.Fatalf("disable: %s %v", msg, err)
	}
	cfg = a.snapshot()["config"].(Config)
	if cfg.LocalAIEnabled {
		t.Fatal("local AI should be off")
	}
	mu.Lock()
	defer mu.Unlock()
	if calls != 1 {
		t.Fatalf("expected one unload, got %d", calls)
	}
}

func TestMissingModelDoesNotEnableLocalAI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"models": []any{}})
	}))
	defer srv.Close()
	old := ollamaEndpoint
	ollamaEndpoint = srv.URL
	defer func() { ollamaEndpoint = old }()
	a, err := newApp(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_, err = a.setLocalAI(context.Background(), true)
	if err == nil {
		t.Fatal("expected missing model error")
	}
	if a.snapshot()["config"].(Config).LocalAIEnabled {
		t.Fatal("must not enable missing model")
	}
}
