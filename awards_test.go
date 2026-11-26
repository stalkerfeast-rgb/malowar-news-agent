package main

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func coverPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 1280, 720))
	img.Set(0, 0, color.RGBA{10, 20, 30, 255})
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func coverTestRequest(t *testing.T, id string, layout CoverAwards, pngData []byte) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("id", id)
	b, _ := json.Marshal(layout)
	_ = w.WriteField("layout", string(b))
	part, e := w.CreateFormFile("cover", "cover.png")
	if e != nil {
		t.Fatal(e)
	}
	_, _ = part.Write(pngData)
	if e = w.Close(); e != nil {
		t.Fatal(e)
	}
	r := httptest.NewRequest("POST", "/api/cover-awards", &buf)
	r.Header.Set("Content-Type", w.FormDataContentType())
	rec := httptest.NewRecorder()
	testApp.saveCoverAwards(rec, r)
	return rec
}

var testApp *app

func validTestLayout() CoverAwards {
	return CoverAwards{HeroX: 0.04, HeroY: 0.05, HeroWidth: 0.42, Layers: []AwardLayer{{Kind: "pearl", X: 0.50, Y: 0.79, Width: 0.43}}}
}
func TestAwardLayoutValidation(t *testing.T) {
	good := validTestLayout()
	if e := validateCoverAwards(good); e != nil {
		t.Fatal(e)
	}
	bad := good
	bad.Layers = []AwardLayer{{Kind: "pearl", X: .9, Y: .9, Width: .5}}
	if validateCoverAwards(bad) == nil {
		t.Fatal("must reject cropped award")
	}
	bad = good
	bad.Layers = []AwardLayer{good.Layers[0], good.Layers[0]}
	if validateCoverAwards(bad) == nil {
		t.Fatal("must reject duplicate kind")
	}
	if e := imagePNG(coverPNG(t)); e != nil {
		t.Fatal(e)
	}
}
func TestAwardsSaveRestartAndReset(t *testing.T) {
	dir := t.TempDir()
	var e error
	testApp, e = newApp(dir)
	if e != nil {
		t.Fatal(e)
	}
	original := filepath.Join(dir, "orig.png")
	if e = os.WriteFile(original, coverPNG(t), 0600); e != nil {
		t.Fatal(e)
	}
	testApp.state.Items = []*Item{{ID: "test:article", Title: "Test", State: "approved", Verified: true, Images: []string{original}, MediaLabels: []string{"Официальный источник"}}}
	rec := coverTestRequest(t, "test:article", validTestLayout(), coverPNG(t))
	if rec.Code != 200 {
		t.Fatalf("save %d: %s", rec.Code, rec.Body.String())
	}
	item := testApp.state.Items[0]
	if item.State != "new" || item.Verified {
		t.Fatal("must reset approval")
	}
	if item.AwardsOriginal == "" || item.Awards == nil || item.Images[0] == original {
		t.Fatal("must keep source and create rendered image")
	}
	if _, e = os.Stat(item.AwardsOriginal); e != nil {
		t.Fatal(e)
	}
	testApp, e = newApp(dir)
	if e != nil {
		t.Fatal(e)
	}
	item = testApp.state.Items[0]
	if item.Awards == nil || item.AwardsOriginal == "" {
		t.Fatal("layout did not survive restart")
	}
	// Editing a rendered cover again retains the first original, never overprints text.
	rec = coverTestRequest(t, "test:article", validTestLayout(), coverPNG(t))
	if rec.Code != 200 {
		t.Fatalf("repeat save %d: %s", rec.Code, rec.Body.String())
	}
	req := httptest.NewRequest("POST", "/api/cover-awards-reset", bytes.NewBufferString(`{"id":"test:article"}`))
	result := httptest.NewRecorder()
	testApp.resetCoverAwards(result, req)
	if result.Code != 200 {
		t.Fatalf("reset %d: %s", result.Code, result.Body.String())
	}
	if testApp.state.Items[0].Awards != nil || testApp.state.Items[0].AwardsOriginal != "" {
		t.Fatal("reset failed")
	}
	if _, e = os.Stat(testApp.state.Items[0].Images[0]); e != nil {
		t.Fatal(e)
	}
	// Re-apply after reset uses existing original file on Windows too.
	rec = coverTestRequest(t, "test:article", validTestLayout(), coverPNG(t))
	if rec.Code != 200 {
		t.Fatalf("save after reset %d: %s", rec.Code, rec.Body.String())
	}
	testApp.state.Items[0].State = "published"
	rec = coverTestRequest(t, "test:article", validTestLayout(), coverPNG(t))
	if rec.Code != 409 {
		t.Fatalf("published material must be immutable, got %d", rec.Code)
	}
}
