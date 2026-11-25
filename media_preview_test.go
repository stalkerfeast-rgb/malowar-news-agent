package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestLocalImagePreviewSecurityPolicy(t *testing.T) {
	if !strings.Contains(pageContentSecurityPolicy, "img-src 'self' data: blob:") {
		t.Fatalf("preview blob URLs forbidden by CSP: %s", pageContentSecurityPolicy)
	}
	if strings.Contains(pageContentSecurityPolicy, "img-src https:") || strings.Contains(pageContentSecurityPolicy, "img-src *") {
		t.Fatal("CSP must not authorize arbitrary third-party image hosts")
	}
	page, err := webFiles.ReadFile("app/index.html")
	if err != nil {
		t.Fatal(err)
	}
	for _, needle := range []string{"URL.createObjectURL", "URL.revokeObjectURL", "showPreviewError", "Версия 1.3.19"} {
		if !bytes.Contains(page, []byte(needle)) {
			t.Errorf("missing preview mechanism %q", needle)
		}
	}
}
