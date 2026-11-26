package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestVKTokenNeverIncludedInState(t *testing.T) {
	a, err := newApp(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	marker := "TEST_VK_ACCESS_TOKEN_NOT_TO_WRITE_984321"
	if err := a.setToken(marker); err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	err = a.persistLocked()
	a.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	disk, err := os.ReadFile(filepath.Join(a.dataDir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(disk, []byte(marker)) {
		t.Fatal("token leaked to state.json")
	}
	output, err := json.Marshal(a.snapshot())
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(output, []byte(marker)) {
		t.Fatal("token leaked to API snapshot")
	}
}
