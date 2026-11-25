package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testBrowserItem(t *testing.T, a *app) *Item {
	t.Helper()
	id := "browser-post"
	folder := filepath.Join(a.dataDir, "images", safeID(id))
	if err := os.MkdirAll(folder, 0700); err != nil {
		t.Fatal(err)
	}
	pics := []string{}
	for i := 1; i <= 3; i++ {
		name := filepath.Join(folder, string(rune('0'+i))+".png")
		if err := os.WriteFile(name, []byte{0x89, 0x50, 0x4e, 0x47, byte(i)}, 0600); err != nil {
			t.Fatal(err)
		}
		pics = append(pics, name)
	}
	draft := ensureGameHashtags("Подтверждённая новость о стратегии и новой кампании с официальными материалами.", "Новости", "Stellaris")
	it := &Item{ID: id, Game: "Stellaris", State: "approved", Draft: draft, Verified: true, Images: pics, Comment: "OLD COMMENT MUST NOT BE SENT"}
	a.state.Items = []*Item{it}
	return it
}
func TestBrowserPrepareAndManualMarkNoAPI(t *testing.T) {
	a, err := newApp(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	it := testBrowserItem(t, a)
	if !a.state.Config.VKBrowserMode || a.state.Config.VKPostponedOnApproval {
		t.Fatal("Browser mode should be default, old API auto-post disabled")
	}
	a.token = "dummy-token" // Would cause network calls if old queue unexpectedly ran.
	res, err := a.prepareVKBrowser(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	if res.Images != 3 || !strings.Contains(res.Folder, "VK_Export") || res.Community != "https://vk.ru/malowargaming" {
		t.Fatalf("bad export %+v", res)
	}
	b, err := os.ReadFile(filepath.Join(res.Folder, "POST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "#Stellaris") || strings.Contains(string(b), "OLD COMMENT") {
		t.Fatalf("wrong exported body: %s", string(b))
	}
	for _, name := range []string{"01.png", "02.png", "03.png", "README.txt"} {
		if _, err := os.Stat(filepath.Join(res.Folder, name)); err != nil {
			t.Fatal(err)
		}
	}
	if it.VKPostID != 0 || it.State != "approved" || it.VKBrowserPreparedAt.IsZero() {
		t.Fatalf("false publication state: %+v", it)
	}
	if err := a.markVKBrowser(it.ID); err != nil {
		t.Fatal(err)
	}
	if it.State != "vk_browser_marked" || it.VKPostID != 0 {
		t.Fatalf("manual mark falsely claimed VK post: %+v", it)
	}
	a.processVKQueue(time.Now())
	a.processSchedule(time.Now().Add(time.Hour))
	if it.State != "vk_browser_marked" {
		t.Fatal("background publisher changed browser state")
	}
	a2, err := newApp(a.dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if a2.state.Items[0].State != "vk_browser_marked" || a2.state.Items[0].VKPostID != 0 {
		t.Fatal("manual handoff not persisted safely")
	}
	if err := a.unmarkVKBrowser(it.ID); err != nil {
		t.Fatal(err)
	}
	if it.State != "approved" {
		t.Fatal("manual mark undo failed")
	}
}
func TestBrowserPrepareRefusesDuplicatesAndStaleContent(t *testing.T) {
	a, _ := newApp(t.TempDir())
	it := testBrowserItem(t, a)
	for _, s := range []string{"uncertain", "vk_postponed", "published"} {
		it.State = s
		if _, err := a.prepareVKBrowser(it.ID); err == nil {
			t.Fatalf("accepted potentially duplicated post in state %s", s)
		}
	}
	it.State = "approved"
	it.VKPostID = 88
	if _, err := a.prepareVKBrowser(it.ID); err == nil {
		t.Fatal("existing VK ID not protected")
	}
	it.VKPostID = 0
	if _, err := a.prepareVKBrowser(it.ID); err != nil {
		t.Fatal(err)
	}
	it.Draft = strings.Replace(it.Draft, "Подтверждённая", "Изменённая", 1)
	if err := a.markVKBrowser(it.ID); err == nil {
		t.Fatal("stale export accepted as final")
	}
	if _, err := a.prepareVKBrowser(it.ID); err != nil {
		t.Fatal(err)
	}
	if err := a.markVKBrowser(it.ID); err != nil {
		t.Fatal(err)
	}
}
func TestBrowserMigrationStopsOldAPIAutomation(t *testing.T) {
	a, _ := newApp(t.TempDir())
	it := testBrowserItem(t, a)
	a.state.Version = 9
	a.state.Config.VKBrowserMode = false
	a.state.Config.VKPostponedOnApproval = true
	it.State = "vk_pending"
	if err := a.persistLocked(); err != nil {
		t.Fatal(err)
	}
	b, err := newApp(a.dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if b.state.Version != 10 || !b.state.Config.VKBrowserMode || b.state.Config.VKPostponedOnApproval {
		t.Fatal("migration did not disable VK API")
	}
	if b.state.Items[0].State != "vk_pending" {
		t.Fatal("migration erased old queue status")
	}
	b.token = "dummy"
	b.processVKQueue(time.Now())
	if b.state.Items[0].State != "vk_pending" {
		t.Fatal("old VK queue unexpectedly processed")
	}
}
func TestBrowserPlanNoAutoSend(t *testing.T) {
	a, _ := newApp(t.TempDir())
	it := testBrowserItem(t, a)
	tm, err := a.setSchedule(it.ID, "queue", "")
	if err != nil {
		t.Fatal(err)
	}
	if it.State != "approved" || tm.IsZero() {
		t.Fatal("browser plan must not schedule automatic API publishing")
	}
	second := &Item{ID: "second", State: "approved", Verified: true, Images: it.Images}
	a.state.Items = append(a.state.Items, second)
	if _, err := a.setSchedule(second.ID, "at", tm.In(moscow).Format("2006-01-02T15:04")); err == nil {
		t.Fatal("browser planning collision")
	}
}
