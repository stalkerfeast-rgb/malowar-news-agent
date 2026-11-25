package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestMascotAssetsBundled(t *testing.T) {
	for _, path := range []string{"app/mascot.png", "app/favicon.ico"} {
		b, err := webFiles.ReadFile(path)
		if err != nil || len(b) < 1000 {
			t.Fatalf("missing branding %s %v", path, err)
		}
	}
}

func TestSavedLocalAIDoesNotStartWhenOff(t *testing.T) {
	a, err := newApp(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	a.startLocalAIWithSavedConsent()
	if time.Since(start) > time.Second {
		t.Fatal("disabled local AI did not return quickly")
	}
}

func TestEnsureOllamaDaemonAlreadyAvailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]any{"models": []map[string]string{{"name": "qwen3:14b"}}})
	}))
	defer srv.Close()
	prev := ollamaEndpoint
	ollamaEndpoint = srv.URL
	defer func() { ollamaEndpoint = prev }()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	models, err := ensureOllamaDaemon(ctx)
	if err != nil || !containsOllamaModel(models, "qwen3:14b") {
		t.Fatalf("models %v %v", models, err)
	}
}

func TestBrandingNotHtmlScript(t *testing.T) {
	b, _ := webFiles.ReadFile("app/index.html")
	if !bytes.Contains(b, []byte("/assets/mascot.png")) || !bytes.Contains(b, []byte("/favicon.ico")) {
		t.Fatal("mascot missing from user interface")
	}
	if !bytes.Contains(b, []byte("Версия 1.3.19")) {
		t.Fatal("wrong UI version")
	}
}
