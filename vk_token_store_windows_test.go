//go:build windows

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestDPAPIVaultRoundTrip(t *testing.T) {
	file := filepath.Join(t.TempDir(), "secret.dpapi")
	secret := "VK_TEST_TOKEN_NEVER_LOG"
	if err := writeVKCredential(file, secret); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(secret)) {
		t.Fatal("token in plaintext")
	}
	out, err := readVKCredential(file)
	if err != nil || out != secret {
		t.Fatalf("vault roundtrip failed: %v", err)
	}
	if err := writeVKCredential(file, ""); err != nil {
		t.Fatal(err)
	}
	out, err = readVKCredential(file)
	if err != nil || out != "" {
		t.Fatal("token not removed")
	}
}
