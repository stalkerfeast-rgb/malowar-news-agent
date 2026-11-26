package main

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestApprovedLegacyMustBeExplicitlyEnqueued(t *testing.T) {
	a, err := newApp(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a.state.Config.VKBrowserMode = false
	a.state.Config.VKPostponedOnApproval = true
	a.state.Config.GroupID = "1234"
	draft := ensureGameHashtags("Достоверное сообщение о большой стратегии, проверенное редактором и по источнику.", "Новости", "Age of Wonders 4")
	post := &Item{ID: "old", State: "approved", Verified: true, Game: "Age of Wonders 4", Draft: draft, Comment: "https://example.org", Images: []string{"a", "b", "c"}}
	a.state.Items = []*Item{post}
	if post.State != "approved" {
		t.Fatal("legacy item unexpectedly queued")
	}
	when, err := a.enqueueExistingApproved("old")
	if err != nil {
		t.Fatal(err)
	}
	if !when.After(time.Now()) || post.State != "vk_pending" {
		t.Fatalf("state=%v time=%v", post.State, when)
	}
	if _, err = a.enqueueExistingApproved("old"); err == nil {
		t.Fatal("double enqueue accepted")
	}
	b, err := newApp(a.dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if b.state.Items[0].State != "vk_pending" || b.state.Items[0].VKPostID != 0 {
		t.Fatal("queued status not retained safely")
	}
}
func TestScheduleGoesToVKQueueOnlyWhenEnabled(t *testing.T) {
	a, e := newApp(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	a.state.Config.VKBrowserMode = false
	a.state.Config.VKPostponedOnApproval = true
	a.state.Config.GroupID = "4321"
	draft := ensureGameHashtags("Обзор стратегии с подтверждёнными данными из первоисточника.", "Новости", "Stellaris")
	a.state.Items = []*Item{{ID: "x", State: "approved", Verified: true, Game: "Stellaris", Draft: draft, Comment: "https://example.org", Images: []string{"1", "2", "3"}}}
	when, e := a.setSchedule("x", "queue", "")
	if e != nil {
		t.Fatal(e)
	}
	if a.state.Items[0].State != "vk_pending" || !when.After(time.Now()) {
		t.Fatal("VK schedule not queued")
	}
	if a.state.Items[0].VKPostID != 0 {
		t.Fatal("VK record ID invented")
	}
	if _, e = a.setSchedule("x", "queue", ""); e == nil {
		t.Fatal("duplicate schedule accepted")
	}
}
func TestVKDefinitiveAPIRejectionIsNotAmbiguous(t *testing.T) {
	old := client
	defer func() { client = old }()
	client = &http.Client{Transport: mockRoundTripper(func(req *http.Request) (*http.Response, error) {
		formData, _ := io.ReadAll(req.Body)
		_ = formData
		var msg string
		switch {
		case strings.Contains(req.URL.Path, "photos.getWallUploadServer"):
			msg = `{"response":{"upload_url":"https://upload.localhost/photo"}}`
		case req.URL.Host == "upload.localhost":
			msg = `{"photo":"abc","server":1,"hash":"hash"}`
		case strings.Contains(req.URL.Path, "photos.saveWallPhoto"):
			msg = `{"response":[{"owner_id":-1,"id":10}]}`
		case strings.Contains(req.URL.Path, "wall.post"):
			msg = `{"error":{"error_code":7,"error_msg":"Permission to perform this action is denied"}}`
		default:
			t.Errorf("unexpected request: %s", req.URL)
			msg = `{"error":{"error_code":1,"error_msg":"bad"}}`
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(msg)), Header: http.Header{}}, nil
	})}
	a, e := newApp(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	imgs := []string{}
	for i := 0; i < 3; i++ {
		n := filepath.Join(a.dataDir, fmt.Sprintf("img%d.png", i))
		if e = os.WriteFile(n, []byte("demo"), 0600); e != nil {
			t.Fatal(e)
		}
		imgs = append(imgs, n)
	}
	it := &Item{ID: "test", State: "vk_sending", Images: imgs, Comment: "https://example.org", Draft: ensureGameHashtags("Достоверная новость о стратегии, подготовленная редакцией.", "Новости", "Stellaris"), Game: "Stellaris", Verified: true, ScheduledAt: time.Now().Add(5 * time.Hour)}
	a.state.Items = []*Item{it}
	a.posting = true
	a.sendPostponed(*it, "token", "1")
	if it.State != "vk_pending_error" || it.VKPostID != 0 || !strings.Contains(it.VKQueueError, "VK API 7") {
		t.Fatalf("incorrect response to rejection: %+v", it)
	}
	if !errors.As(&VKAPIError{Code: 7, Msg: "permission"}, new(*VKAPIError)) {
		t.Fatal("typed VK error not working")
	}
	_ = url.Values{}
}
