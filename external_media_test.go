package main

import (
	"net/url"
	"testing"
)

func TestArticleImageURLUsesPreviewAndRejectsUnsafeScheme(t *testing.T) {
	origin, err := url.Parse("https://developer.example/updates/news")
	if err != nil {
		t.Fatal(err)
	}
	body := []byte(`<meta property="og:image" content="file:///example.png"><meta content="/art/post.png?x=1&amp;y=2" property="og:image">`)
	if got := articleImageURL(body, origin); got != "https://developer.example/art/post.png?x=1&y=2" {
		t.Fatalf("preview URL = %q", got)
	}
}
