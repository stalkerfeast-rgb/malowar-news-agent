package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGameHashtagAndAIRepair(t *testing.T) {
	cases := map[string]string{"Age of Wonders 4": "#AgeofWonders4", "Total War: Warhammer III": "#TotalWarWarhammerIII", "Герои Меча и Магии III": "#ГероиМечаиМагииIII"}
	for name, tag := range cases {
		if got := gameHashtag(name); got != tag {
			t.Fatalf("%s: %s != %s", name, got, tag)
		}
		original := "Новая стратегия с подробным описанием.\n\n#НовостиИгр #MalowarGaming #Стратегии #Steam"
		fixed := ensureGameHashtags(original, "Стратегии", name)
		if !hasGameHashtags(fixed, name) || !strings.Contains(fixed, tag) {
			t.Fatalf("missing game hashtag: %s", fixed)
		}
		if ensureGameHashtags(fixed, "Стратегии", name) != fixed {
			t.Fatal("non-idempotent repair")
		}
		if hasGameHashtags(original, name) {
			t.Fatal("bad validation")
		}
	}
}
func TestCatalog50AndCustomAsset(t *testing.T) {
	a, err := newApp(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(a.state.AwardCatalog) != 50 {
		t.Fatalf("want 50 awards, got %d", len(a.state.AwardCatalog))
	}
	for n, v := range a.state.AwardCatalog {
		if n < 3 && v.Status != "ready" {
			t.Fatal("3 approved PNGs missing")
		}
		if n >= 3 && v.Status != "candidate" {
			t.Fatal("draft art incorrectly labeled as approved")
		}
	}
	if err = a.addAwardCandidate("Новая категория", "Редакция"); err != nil {
		t.Fatal(err)
	}
	id := a.state.AwardCatalog[len(a.state.AwardCatalog)-1].ID
	img := image.NewRGBA(image.Rect(0, 0, 300, 46))
	for y := 0; y < 46; y++ {
		for x := 0; x < 300; x++ {
			img.Set(x, y, color.RGBA{45, 64, 96, 255})
		}
	}
	var b bytes.Buffer
	if err = png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	if err = a.uploadAwardPNG(id, bytes.NewReader(b.Bytes())); err != nil {
		t.Fatal(err)
	}
	if _, err = a.awardPNG(id); err != nil {
		t.Fatal(err)
	}
	if err = validateCoverAwards(CoverAwards{HeroWidth: 0.4, Layers: []AwardLayer{{Kind: id, X: .5, Y: .6, Width: .3}}}); err != nil {
		t.Fatal(err)
	}
	bApp, err := newApp(a.dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if bApp.awardCatalogLocked(id) == nil || bApp.awardCatalogLocked(id).Status != "ready" {
		t.Fatal("asset not saved")
	}
}
func TestNewApprovalEnqueuesButLegacyApprovalPreserved(t *testing.T) {
	a, err := newApp(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a.state.Config.VKBrowserMode = false
	a.state.Config.VKPostponedOnApproval = true
	a.state.Config.GroupID = "12345"
	draft := ensureGameHashtags("Подробная официальная новость о стратегии, подтверждённая разработчиками и редакцией.", "Новости", "Age of Wonders 4")
	it := &Item{ID: "x", State: "new", Game: "Age of Wonders 4", Title: "Релиз", URL: "https://example.org", Draft: draft, Images: []string{"1.png", "2.png", "3.png"}, Verified: true, Comment: "https://example.org"}
	a.state.Items = []*Item{it}
	if err = a.updateItem("x", draft, it.Comment, "approved", true); err != nil {
		t.Fatal(err)
	}
	if it.State != "vk_pending" || !it.ScheduledAt.After(time.Now()) {
		t.Fatalf("not queued: %s %v", it.State, it.ScheduledAt)
	}
	if it.VKPostID != 0 {
		t.Fatal("sent to VK without token")
	}
	b, err := newApp(a.dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if b.state.Items[0].State != "vk_pending" {
		t.Fatal("lost queue on restart")
	}
	if err = b.cancelVKQueue("x"); err != nil {
		t.Fatal(err)
	}
	if b.state.Items[0].State != "approved" {
		t.Fatal("failed to cancel local queue")
	}
	// Migration must not enqueue pre-existing approved drafts retroactively.
	b.state.Version = 8
	b.state.Items[0].State = "approved"
	if err = b.persistLocked(); err != nil {
		t.Fatal(err)
	}
	c, err := newApp(a.dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if c.state.Items[0].State != "approved" {
		t.Fatal("old draft silently submitted")
	}
}
func TestVKPostponedFlowMocked(t *testing.T) {
	oldClient := client
	defer func() { client = oldClient }()
	calls := []string{}
	postOK := false
	client = &http.Client{Transport: mockRoundTripper(func(req *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(req.Body)
		form, _ := url.ParseQuery(string(body))
		calls = append(calls, req.URL.String())
		var out string
		switch {
		case strings.Contains(req.URL.Path, "photos.getWallUploadServer"):
			out = `{"response":{"upload_url":"https://upload.test/asset"}}`
		case req.URL.Host == "upload.test":
			out = `{"photo":"abc","server":123,"hash":"xyz"}`
		case strings.Contains(req.URL.Path, "photos.saveWallPhoto"):
			out = `{"response":[{"id":77,"owner_id":-4}]}`
		case strings.Contains(req.URL.Path, "wall.post"):
			if form.Get("publish_date") == "" {
				t.Error("missing VK publish_date")
			}
			if !strings.Contains(form.Get("message"), "#AgeofWonders4") {
				t.Error("missing game hashtag")
			}
			postOK = true
			out = `{"response":{"post_id":99}}`
		case strings.Contains(req.URL.Path, "wall.getById"):
			out = `{"response":[{"id":99,"post_type":"post","date":1732419000}]}`
		case strings.Contains(req.URL.Path, "wall.createComment"):
			if form.Get("message") != "https://example.org" {
				t.Error("source not in comment")
			}
			out = `{"response":{"comment_id":100}}`
		default:
			t.Errorf("unexpected request %s", req.URL)
			out = `{"error":{"error_code":1,"error_msg":"unknown"}}`
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(out)), Header: make(http.Header)}, nil
	})}
	a, err := newApp(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var images []string
	for i := 0; i < 3; i++ {
		f := filepath.Join(a.dataDir, fmt.Sprintf("picture%d.png", i))
		os.WriteFile(f, []byte("dummy"), 0600)
		images = append(images, f)
	}
	it := &Item{ID: "a", Game: "Age of Wonders 4", State: "vk_sending", Verified: true, Draft: ensureGameHashtags("Новый официальный релиз стратегии, подтверждённый разработчиками.", "Новости", "Age of Wonders 4"), Images: images, Comment: "https://example.org", ScheduledAt: time.Now().Add(3 * time.Hour)}
	a.state.Items = []*Item{it}
	a.posting = true
	a.sendPostponed(*it, "test-token", "4")
	if !postOK || it.State != "vk_postponed" || it.VKPostID != 99 || it.CommentState != "waiting" {
		t.Fatalf("VK was not scheduled: %+v", it)
	}
	for _, v := range calls {
		if strings.Contains(v, "wall.createComment") {
			t.Fatal("comment created before publication")
		}
	}
	a.posting = true
	a.verifyPublishedPost("a", 99, "test-token", "4")
	if it.State != "published" || it.CommentID != 100 || it.CommentState != "sent" {
		t.Fatalf("comment missing after publish: %+v", it)
	}
	_ = json.Valid([]byte("{}"))
	_ = context.Background()
}
