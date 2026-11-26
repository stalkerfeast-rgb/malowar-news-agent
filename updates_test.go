package main

import "testing"

func TestVersionCompare(t *testing.T) {
	for _, tt := range []struct {
		a, b string
		want int
	}{{"1.3.3", "v1.3.4", -1}, {"1.4.0", "1.3.99", 1}, {"1.3.3", "1.3.3", 0}} {
		if got := compareVersions(tt.a, tt.b); got != tt.want {
			t.Fatalf("%s %s: %d", tt.a, tt.b, got)
		}
	}
}
func TestTrustedUpdateURLs(t *testing.T) {
	if !githubAssetURL("https://github.com/owner/repo/releases/download/v1.3.4/Malowar_News_Agent_Update.zip", "owner/repo") {
		t.Fatal("valid url")
	}
	if githubAssetURL("https://evil.com/owner/repo/releases/download/v1.3.4/update.zip", "owner/repo") {
		t.Fatal("untrusted url")
	}
	if !validUpdateRepo("owner/repo") || validUpdateRepo("../../bad") {
		t.Fatal("repo validation")
	}
}
