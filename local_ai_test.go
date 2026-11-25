package main

import (
	"testing"
)

func TestLocalModelDefaults(t *testing.T) {
	for _, input := range []string{"", "qwen2.5:7b", "gpt-4.1-mini"} {
		if out := effectiveLocalModel(Config{AIModel: input}); out != "qwen3:14b" {
			t.Fatalf("%q => %q", input, out)
		}
	}
	if effectiveLocalModel(Config{AIModel: "qwen3:8b"}) != "qwen3:8b" {
		t.Fatal("custom model lost")
	}
}
func TestLocalAIUpgradeDisabled(t *testing.T) {
	d := t.TempDir()
	a, err := newApp(d)
	if err != nil {
		t.Fatal(err)
	}
	if a.state.Config.LocalAIEnabled {
		t.Fatal("local AI should be disabled by default")
	}
	if a.state.Config.AutoApproved {
		t.Fatal("publishing must be gated")
	}
}
func TestModelMatching(t *testing.T) {
	if !containsOllamaModel([]string{"qwen3:14b"}, "qwen3:14b") {
		t.Fatal("matching failed")
	}
	if containsOllamaModel([]string{"qwen3:8b"}, "qwen3:14b") {
		t.Fatal("matched wrong model")
	}
}
