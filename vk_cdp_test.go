package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestVKCommunityNormalization(t *testing.T) {
	cases := map[string]string{"malowargaming": "https://vk.ru/malowargaming", "https://vk.com/club211159240": "https://vk.ru/club211159240", "https://vk.ru/malowargaming": "https://vk.ru/malowargaming"}
	for input, want := range cases {
		got, err := normalizeVKCommunity(input)
		if err != nil || got != want {
			t.Fatalf("%q got %q: %v", input, got, err)
		}
	}
	for _, bad := range []string{"https://evil.example/malowargaming", "https://vk.ru/feed", "https://vk.ru/malowargaming/other", "https://vk.ru/malowargaming?act=edit", "http://vk.ru/malowargaming", "https://vk.ru@evil.example/malowargaming"} {
		if _, err := normalizeVKCommunity(bad); err == nil {
			t.Errorf("allowed unsafe community URL: %s", bad)
		}
	}
}
func TestVKCDPOnLocalMock(t *testing.T) {
	if os.Getenv("MNA_LIVE_CDP_TEST") != "1" {
		t.Skip("Live Chromium integration is opt-in; run on a suitable desktop")
	}
	chrome, e := exec.LookPath("chromium")
	if e != nil {
		t.Skip("Chromium unavailable")
	}
	dir, mkErr := os.MkdirTemp("", "mna-cdp-test-")
	if mkErr != nil {
		t.Fatal(mkErr)
	}
	defer func() {
		for i := 0; i < 15; i++ {
			if os.RemoveAll(dir) == nil {
				break
			}
			time.Sleep(120 * time.Millisecond)
		}
	}()
	profile := filepath.Join(dir, "profile")
	os.MkdirAll(profile, 0700)

	html := `<!DOCTYPE html><meta charset="utf-8"><div role="textbox" contenteditable="true" style="width:400px;height:80px"></div><input type="file" multiple accept="image/*" id="p"><span id="num">0</span><script>document.getElementById('p').addEventListener('change',e=>document.getElementById('num').textContent=e.target.files.length)</script>`
	mock := filepath.Join(dir, "mock.html")
	if e := os.WriteFile(mock, []byte(html), 0600); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := exec.CommandContext(ctx, chrome, "--headless=new", "--no-sandbox", "--disable-dev-shm-usage", "--disable-gpu", "--no-first-run", "--no-proxy-server", "--remote-debugging-port=0", "--user-data-dir="+profile, "about:blank")
	if e := cmd.Start(); e != nil {
		t.Skipf("cannot start Chromium: %v", e)
	}
	defer func() { cancel(); _ = cmd.Wait() }()
	var port int
	var target vkCDPTarget
	for i := 0; i < 75; i++ {
		if b, e := os.ReadFile(filepath.Join(profile, "DevToolsActivePort")); e == nil {
			fmt.Sscanf(string(b), "%d", &port)
		}
		if port > 0 {
			resp, e := http.Get(fmt.Sprintf("http://127.0.0.1:%d/json/list", port))
			if e == nil {
				var ts []vkCDPTarget
				_ = json.NewDecoder(resp.Body).Decode(&ts)
				resp.Body.Close()
				for _, x := range ts {
					if x.URL == "about:blank" && x.Type == "page" {
						target = x
						break
					}
				}
			}
		}
		if target.WS != "" {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if target.WS == "" {
		t.Skip("Chromium debugging endpoint unavailable in container")
	}
	c, e := dialCDPSocket(target.WS)
	if e != nil {
		t.Fatal(e)
	}
	defer c.close()
	// Container Chromium blocks data: URLs on some sandboxed systems.
	// Populate the trusted local about:blank test page directly instead.
	bHTML, _ := json.Marshal(html)
	if _, e := cdpEvaluate(c, "(function(){document.open();document.write("+string(bHTML)+");document.close();return {ok:true}})()"); e != nil {
		t.Fatal(e)
	}
	// Wait until the loaded browser document has mounted the editor.
	for i := 0; i < 30; i++ {
		r, err := cdpEvaluate(c, "({ready:document.readyState,editors:document.querySelectorAll('[contenteditable]').length})")
		if err == nil && r["editors"] == float64(1) && r["ready"] == "complete" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	text := `Тест — "Стратегия" #Stellaris`
	b, _ := json.Marshal(text)
	result, e := cdpEvaluate(c, strings.Replace(vkFillTextJS, "__TEXT__", string(b), 1))
	if e != nil {
		t.Fatal(e)
	}
	if result["ok"] != true {
		debug, _ := cdpEvaluate(c, "({url:location.href,ready:document.readyState,html:document.documentElement.outerHTML.slice(0,900),editors:document.querySelectorAll('[contenteditable]').length})")
		t.Fatalf("text fill failed: %v, debug: %v", result, debug)
	}
	result, e = cdpEvaluate(c, "({value:document.querySelector('[contenteditable]').textContent})")
	if e != nil {
		t.Fatal(e)
	}
	if result["value"] != text {
		t.Fatalf("wrong editor value %v", result)
	}
	result, e = cdpEvaluate(c, vkFindImageInputJS)
	if e != nil || result["ok"] != true {
		t.Fatalf("missing file input: %v %v", result, e)
	}
	obj, e := cdpImageInputObject(c)
	if e != nil {
		t.Fatal(e)
	}
	var paths []string
	for i := 0; i < 3; i++ {
		f := filepath.Join(dir, fmt.Sprintf("%d.png", i))
		if e = os.WriteFile(f, []byte("fake content"), 0600); e != nil {
			t.Fatal(e)
		}
		paths = append(paths, f)
	}
	if _, e = c.call("DOM.setFileInputFiles", map[string]any{"files": paths, "objectId": obj}); e != nil {
		t.Fatal(e)
	}
	result, e = cdpEvaluate(c, "({count:document.getElementById('p').files.length,changed:document.getElementById('num').textContent})")
	if e != nil {
		t.Fatal(e)
	}
	if result["count"] != float64(3) {
		t.Fatalf("file input not populated: %v", result)
	}

	// One-click launch must open a *community* post composer, without touching
	// the final publication button. The existing text and file routines then run.
	htmlLaunch := `<!DOCTYPE html><meta charset="utf-8"><main><button id="new">Создать запись</button><button id="publish">Опубликовать</button><div id="mount"></div></main><script>window.published=0;document.getElementById('publish').onclick=()=>window.published++;document.getElementById('new').onclick=()=>{document.getElementById('mount').innerHTML='<div role="dialog" aria-label="Создать запись"><div contenteditable="true" aria-label="Текст записи" style="width:450px;height:90px;border:1px solid black"></div></div>'}</script>`
	bLaunch, _ := json.Marshal(htmlLaunch)
	if _, e = cdpEvaluate(c, "(function(){document.open();document.write("+string(bLaunch)+");document.close();return {ok:true}})()"); e != nil {
		t.Fatal(e)
	}
	if e = cdpOpenVKComposer(c); e != nil {
		t.Fatalf("one-click composer could not open: %v", e)
	}
	launcher, e := cdpEvaluate(c, "({editor:!!document.querySelector('[role=dialog] [contenteditable]'),published:window.published})")
	if e != nil || launcher["editor"] != true || launcher["published"] != float64(0) {
		t.Fatalf("one-click opened wrong element or published: %v %v", launcher, e)
	}
	textLaunch, _ := json.Marshal("Автоматически открытая запись #Stellaris")
	filled, e := cdpEvaluate(c, strings.Replace(vkFillTextJS, "__TEXT__", string(textLaunch), 1))
	if e != nil || filled["ok"] != true {
		t.Fatalf("could not fill opened composer: %v %v", filled, e)
	}
	launcher, e = cdpEvaluate(c, "({text:document.querySelector('[role=dialog] [contenteditable]').innerText,published:window.published})")
	if e != nil || launcher["text"] != "Автоматически открытая запись #Stellaris" || launcher["published"] != float64(0) {
		t.Fatalf("wrong one-click result: %v %v", launcher, e)
	}

	// Ambiguous composer controls must not be clicked, especially when there
	// are other actions on the same page.
	htmlAmbiguous := `<!DOCTYPE html><meta charset="utf-8"><main><button id="a">Создать запись</button><button id="b">Создать запись</button><button id="publish">Опубликовать</button></main><script>window.clicked=0;for(const b of document.querySelectorAll('button'))b.onclick=()=>window.clicked++</script>`
	bAmbiguous, _ := json.Marshal(htmlAmbiguous)
	if _, e = cdpEvaluate(c, "(function(){document.open();document.write("+string(bAmbiguous)+");document.close();return {ok:true}})()"); e != nil {
		t.Fatal(e)
	}
	if e = cdpOpenVKComposer(c); e == nil {
		t.Fatal("ambiguous composer buttons should be rejected")
	}
	ambiguous, e := cdpEvaluate(c, "({clicked:window.clicked})")
	if e != nil || ambiguous["clicked"] != float64(0) {
		t.Fatalf("ambiguous composer clicked unintended element: %v %v", ambiguous, e)
	}

	// VK may insert its file input only after the editor's Фото button is
	// clicked. Confirm native chooser interception, without publishing.
	html2 := `<!DOCTYPE html><meta charset="utf-8"><div role="dialog" aria-label="Создать запись"><div contenteditable="true" aria-label="Текст записи" style="width:450px;height:90px;border:1px solid black"></div><button id="photo" type="button">Фото</button><span id="num">0</span></div><script>document.getElementById('photo').addEventListener('click',()=>{let f=document.createElement('input');f.type='file';f.multiple=true;f.accept='image/*';f.id='chosen';f.addEventListener('change',()=>{document.getElementById('num').textContent=f.files.length});document.body.appendChild(f);f.click()})</script>`
	b2, _ := json.Marshal(html2)
	_, e = cdpEvaluate(c, "(function(){document.open();document.write("+string(b2)+");document.close();return {ok:true}})()")
	if e != nil {
		t.Fatal(e)
	}
	txt, _ := json.Marshal("Новая тестовая запись #Stellaris")
	result, e = cdpEvaluate(c, strings.Replace(vkFillTextJS, "__TEXT__", string(txt), 1))
	if e != nil || result["ok"] != true {
		t.Fatalf("could not set text: %v, %v", result, e)
	}
	method, e := cdpAttachVKPhotos(c, paths)
	if e != nil {
		t.Fatalf("dynamic input/chooser not filled: %v", e)
	}
	if method == "" {
		t.Fatal("missing method")
	}
	result, e = cdpEvaluate(c, "({count:document.getElementById('chosen')?.files.length||0,changed:document.getElementById('num').textContent})")
	if e != nil || result["count"] != float64(3) {
		t.Fatalf("dynamic photo input incomplete: %v %v", result, e)
	}

	// A VK-like drop target can receive file paths directly via Chromium.
	// No file input is required, but dragover must be explicitly accepted.
	html3 := `<!DOCTYPE html><meta charset="utf-8"><div contenteditable="true" aria-label="Текст записи" style="width:470px;height:100px;border:1px solid black"></div><div id="drop" style="width:500px;height:140px;border:2px solid gray">Зона фото</div><script>const zone=document.querySelector('[contenteditable]');zone.addEventListener('dragover',e=>e.preventDefault());zone.addEventListener('drop',e=>{e.preventDefault();document.getElementById('drop').textContent='photos='+e.dataTransfer.files.length})</script>`
	b3, _ := json.Marshal(html3)
	_, e = cdpEvaluate(c, "(function(){document.open();document.write("+string(b3)+");document.close();return {ok:true}})()")
	if e != nil {
		t.Fatal(e)
	}
	txt, _ = json.Marshal("Проверяем перетаскивание")
	result, e = cdpEvaluate(c, strings.Replace(vkFillTextJS, "__TEXT__", string(txt), 1))
	if e != nil || result["ok"] != true {
		t.Fatalf("drop editor text: %v %v", result, e)
	}
	if e = cdpDragVKPhotos(c, paths); e != nil {
		t.Fatalf("CDP file drag failed: %v", e)
	}
	result, e = cdpEvaluate(c, "({dropped:document.getElementById('drop').textContent})")
	if e != nil || result["dropped"] != "photos=3" {
		t.Fatalf("dropped files not received: %v %v", result, e)
	}

	// Do not drop on a page that has no explicit file-drop handler.
	html4 := `<!DOCTYPE html><meta charset="utf-8"><div contenteditable="true" aria-label="Текст записи" style="width:470px;height:100px;border:1px solid black"></div>`
	b4, _ := json.Marshal(html4)
	_, e = cdpEvaluate(c, "(function(){document.open();document.write("+string(b4)+");document.close();return {ok:true}})()")
	if e != nil {
		t.Fatal(e)
	}
	txt, _ = json.Marshal("Нет загрузчика")
	result, e = cdpEvaluate(c, strings.Replace(vkFillTextJS, "__TEXT__", string(txt), 1))
	if e != nil || result["ok"] != true {
		t.Fatalf("guard editor text: %v %v", result, e)
	}
	if e = cdpDragVKPhotos(c, paths); e == nil {
		t.Fatal("drag must be cancelled when no drop target exists")
	}
}

// A publisher must never attach to arbitrary websites or a different community.
func TestVKEditorLocationSafety(t *testing.T) {
	wanted := "https://vk.ru/malowargaming"
	for _, good := range []string{"https://vk.ru/malowargaming", "https://vk.com/malowargaming/"} {
		if !validVKEditorLocation(good, wanted) {
			t.Errorf("rejected community page %q", good)
		}
	}
	for _, bad := range []string{"https://evil.example/malowargaming", "https://vk.ru/another-community", "https://vk.com/feed", "http://vk.ru/malowargaming", "https://vk.ru.evil.test/malowargaming"} {
		if validVKEditorLocation(bad, wanted) {
			t.Errorf("accepted unrelated page %q", bad)
		}
	}
}
func TestVKAutoFillSafetyGuards(t *testing.T) {
	if !strings.Contains(vkFillTextJS, "уже есть другой текст") || !strings.Contains(vkFindImageInputJS, "уже выбраны фотографии") {
		t.Fatal("duplicate protection missing")
	}
	if strings.Contains(vkFillTextJS, "Опубликовать") || strings.Contains(vkFindImageInputJS, "Опубликовать") {
		t.Fatal("automation should never publish")
	}
	a, err := newApp(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	it := testBrowserItem(t, a)
	if _, err := a.autoFillVK(it.ID); err == nil {
		t.Fatal("auto-fill must require explicit prepared approval")
	}
}

func TestVKAutoFillCannotResendAlreadySelectedPhotos(t *testing.T) {
	a, err := newApp(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	it := testBrowserItem(t, a)
	if _, err := a.prepareVKBrowser(it.ID); err != nil {
		t.Fatal(err)
	}
	it.VKBrowserAutoFillAt = time.Now().UTC()
	it.VKBrowserAutoPhotos = true
	if _, err := a.autoFillVK(it.ID); err == nil || !strings.Contains(err.Error(), "против дублей") {
		t.Fatalf("expected duplicate protection, got %v", err)
	}
}
