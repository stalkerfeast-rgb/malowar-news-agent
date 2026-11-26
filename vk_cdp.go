package main

import (
	"bufio"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Chromium DevTools is used only on a dedicated local Edge profile and only
// when the editor explicitly presses Auto-fill. No VK API tokens are used.
var vkAutoFillMu sync.Mutex

type cdpEvent struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type cdpSocket struct {
	conn   net.Conn
	reader *bufio.Reader
	serial int
	events []cdpEvent
}

func dialCDPSocket(raw string) (*cdpSocket, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "ws" || u.Hostname() != "127.0.0.1" || u.User != nil {
		return nil, errors.New("Ожидался локальный канал редактора Edge")
	}
	conn, err := net.DialTimeout("tcp", u.Host, 3*time.Second)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, 16)
	if _, err = rand.Read(nonce); err != nil {
		conn.Close()
		return nil, err
	}
	key := base64.StdEncoding.EncodeToString(nonce)
	path := u.RequestURI()
	_, err = fmt.Fprintf(conn, "GET %s HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Key: %s\r\n\r\n", path, u.Host, key)
	if err != nil {
		conn.Close()
		return nil, err
	}
	reader := bufio.NewReader(conn)
	_ = conn.SetReadDeadline(time.Now().Add(4 * time.Second))
	resp, err := http.ReadResponse(reader, nil)
	if err != nil {
		conn.Close()
		return nil, err
	}
	acceptHash := sha1.Sum([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	if resp.StatusCode != 101 || resp.Header.Get("Sec-WebSocket-Accept") != base64.StdEncoding.EncodeToString(acceptHash[:]) {
		conn.Close()
		return nil, fmt.Errorf("Edge отказал в соединении: HTTP %d", resp.StatusCode)
	}
	return &cdpSocket{conn: conn, reader: reader}, nil
}
func (c *cdpSocket) close() { _ = c.conn.Close() }
func (c *cdpSocket) sendFrame(op byte, data []byte) error {
	mask := make([]byte, 4)
	if _, err := rand.Read(mask); err != nil {
		return err
	}
	head := []byte{0x80 | op}
	n := len(data)
	switch {
	case n < 126:
		head = append(head, 0x80|byte(n))
	case n <= 65535:
		head = append(head, 0x80|126, byte(n>>8), byte(n))
	default:
		head = append(head, 0x80|127)
		var size [8]byte
		binary.BigEndian.PutUint64(size[:], uint64(n))
		head = append(head, size[:]...)
	}
	head = append(head, mask...)
	payload := make([]byte, n)
	for i, b := range data {
		payload[i] = b ^ mask[i%4]
	}
	_, err := c.conn.Write(append(head, payload...))
	return err
}
func (c *cdpSocket) readFrame() (byte, []byte, error) {
	var h [2]byte
	if _, err := io.ReadFull(c.reader, h[:]); err != nil {
		return 0, nil, err
	}
	if h[0]&0x80 == 0 {
		return 0, nil, errors.New("Неподдерживаемое фрагментированное сообщение Edge")
	}
	if h[1]&0x80 != 0 {
		return 0, nil, errors.New("Edge прислал некорректный WebSocket-кадр")
	}
	n := uint64(h[1] & 127)
	if n == 126 {
		var b [2]byte
		if _, err := io.ReadFull(c.reader, b[:]); err != nil {
			return 0, nil, err
		}
		n = uint64(binary.BigEndian.Uint16(b[:]))
	}
	if n == 127 {
		var b [8]byte
		if _, err := io.ReadFull(c.reader, b[:]); err != nil {
			return 0, nil, err
		}
		n = binary.BigEndian.Uint64(b[:])
	}
	if n > 8<<20 {
		return 0, nil, errors.New("Ответ Edge слишком большой")
	}
	data := make([]byte, int(n))
	if _, err := io.ReadFull(c.reader, data); err != nil {
		return 0, nil, err
	}
	return h[0] & 0x0f, data, nil
}
func (c *cdpSocket) call(method string, params any) (json.RawMessage, error) {
	c.serial++
	callID := c.serial
	cmd, err := json.Marshal(map[string]any{"id": callID, "method": method, "params": params})
	if err != nil {
		return nil, err
	}
	_ = c.conn.SetDeadline(time.Now().Add(8 * time.Second))
	if err = c.sendFrame(1, cmd); err != nil {
		return nil, err
	}
	for {
		op, b, err := c.readFrame()
		if err != nil {
			return nil, err
		}
		if op == 9 {
			if err = c.sendFrame(10, b); err != nil {
				return nil, err
			}
			continue
		}
		if op == 8 {
			return nil, errors.New("Браузер закрыл подключение")
		}
		if op != 1 && op != 2 {
			continue
		}
		var r struct {
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
			ID     int             `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err = json.Unmarshal(b, &r); err != nil {
			return nil, err
		}
		if r.Method != "" && len(c.events) < 24 {
			c.events = append(c.events, cdpEvent{Method: r.Method, Params: r.Params})
		}
		if r.ID != callID {
			continue
		}
		if r.Error != nil {
			return nil, errors.New(r.Error.Message)
		}
		return r.Result, nil
	}
}

type vkCDPTarget struct {
	Type string `json:"type"`
	URL  string `json:"url"`
	WS   string `json:"webSocketDebuggerUrl"`
}

func validVKEditorLocation(raw, expected string) bool {
	u, e := url.Parse(raw)
	v, ee := url.Parse(expected)
	if e != nil || ee != nil || u.Scheme != "https" || (u.Hostname() != "vk.com" && u.Hostname() != "vk.ru") {
		return false
	}
	return strings.EqualFold(strings.TrimRight(u.Path, "/"), strings.TrimRight(v.Path, "/"))
}
func connectVKEditor(expected string) (*cdpSocket, error) {
	profile := vkBrowserProfilePath()
	b, err := os.ReadFile(filepath.Join(profile, "DevToolsActivePort"))
	if err != nil {
		return nil, errors.New("Браузер VK открыт без режима передачи. Закрой отдельное окно VK и открой заново через News Agent")
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) == "" {
		return nil, errors.New("Браузер VK не сообщил порт. Закрой окно VK и открой заново")
	}
	port, err := strconv.Atoi(strings.TrimSpace(lines[0]))
	if err != nil || port < 1024 || port > 65535 {
		return nil, errors.New("Неверный адрес локального окна Edge")
	}
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/json/list", port))
	if err != nil {
		return nil, fmt.Errorf("Нет связи с окном VK: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, errors.New("Edge не разрешил подключиться к редактору")
	}
	var ts []vkCDPTarget
	if err = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&ts); err != nil {
		return nil, err
	}
	for _, t := range ts {
		if t.Type == "page" && validVKEditorLocation(t.URL, expected) && t.WS != "" {
			return dialCDPSocket(t.WS)
		}
	}
	return nil, errors.New("Открой страницу нужного сообщества VK в отдельном окне News Agent")
}

const vkFillTextJS = `(function(){
 const val=__TEXT__;
 function shown(el){const r=el.getBoundingClientRect();const cs=getComputedStyle(el);return r.width>90&&r.height>9&&cs.visibility!=='hidden'&&cs.display!=='none';}
 const editable=(el)=>el&&el.matches&&el.matches('textarea,[contenteditable="true"],[contenteditable="plaintext-only"]');
 function safeCandidate(el){
   if(!editable(el)||!shown(el)||el.closest('header,nav,[role="navigation"],[role="search"]'))return false;
   const info=[el.getAttribute('placeholder'),el.getAttribute('data-placeholder'),el.getAttribute('aria-label'),el.getAttribute('data-testid')].join(' ').toLowerCase();
   if(/поиск|сообщени[яе]|комментари|парол|номер телефон|email|логин/.test(info))return false;
   const modal=el.closest('[role="dialog"]');
   if(modal && /вход|авторизац|регистрац|сообщени[яе]/i.test(modal.getAttribute('aria-label')||''))return false;
   return true;
 }
 const active=document.activeElement;
 const focus=editable(active)?active:active?.closest?.('[contenteditable="true"],[contenteditable="plaintext-only"]');
 const all=[...document.querySelectorAll('textarea,[contenteditable="true"],[contenteditable="plaintext-only"]')].filter(safeCandidate);
 const likely=all.filter(el=>/что.*нов|создать.*запис|напишите|поделит|публикац|пост|новост|текст запис|запис/i.test([el.getAttribute('placeholder'),el.getAttribute('data-placeholder'),el.getAttribute('aria-label'),el.closest('[role="dialog"]')?.getAttribute('aria-label')].join(' ')));
 let target=null;
 if(focus&&safeCandidate(focus)&&(likely.includes(focus)||(all.length===1&&likely.length===0)))target=focus;
 else if(likely.length===1)target=likely[0];
 else if(all.length===1)target=all[0];
 else return {ok:false,reason:'Не найдено безопасное поле записи VK. Открой «Создать запись», кликни в текст поста и повтори. Полей: '+all.length};
 const previous=target.isContentEditable?target.innerText:target.value;
 if(previous.trim()===val.trim()){window.__mnaEditor=target;return {ok:true,reused:true,reason:'Текст уже находится в редакторе VK. Повторно не вставляли.'};}
 if(previous.trim())return {ok:false,reason:'В поле уже есть другой текст. Чтобы не потерять черновик, автоперенос остановлен.'};
 target.focus();
 window.__mnaEditor=target;
 if(target.isContentEditable){
  const ok=document.execCommand('insertText',false,val);
  if(!ok)return {ok:false,reason:'VK не разрешил заполнить текстовое поле'};
 } else {
  const proto=target.tagName==='TEXTAREA'?HTMLTextAreaElement.prototype:HTMLInputElement.prototype;
  const setter=Object.getOwnPropertyDescriptor(proto,'value')?.set;
  if(!setter)return {ok:false,reason:'Редактор VK не поддерживает ввод текста'};
  setter.call(target,val);target.dispatchEvent(new Event('input',{bubbles:true}));target.dispatchEvent(new Event('change',{bubbles:true}));
 }
 return {ok:true,reason:'Текст вставлен. Проверь его перед публикацией.'};
})()`

// A deliberately narrow launcher: only controls describing a new community
// post may be clicked. We never click a publish, schedule, save or submit button.
// VK changes its DOM over time, so ambiguity is a recoverable error, not a cue
// to guess a random clickable element.
const vkOpenComposerJS = `(function(){
 function shown(el){const r=el.getBoundingClientRect(),c=getComputedStyle(el);return r.width>45&&r.height>9&&c.display!=='none'&&c.visibility!=='hidden';}
 function prohibited(el){
  if(el.closest('header,nav,footer,[role="navigation"],[role="search"],aside,[role="menu"],article,[data-post-id]'))return true;
  const dialog=el.closest('[role="dialog"],dialog,[aria-modal="true"]');
  if(dialog&&/вход|авторизац|сообщени|поиск|регистрац/i.test(dialog.getAttribute('aria-label')||''))return true;
  return false;
 }
 const editors=[...document.querySelectorAll('textarea,[contenteditable="true"],[contenteditable="plaintext-only"]')].filter(el=>shown(el)&&!prohibited(el)&&!/поиск|комментари|сообщени|ответить/i.test([el.getAttribute('placeholder'),el.getAttribute('data-placeholder'),el.getAttribute('aria-label')].join(' ')));
 const modal=editors.filter(el=>{
  const box=el.closest('[role="dialog"],dialog,[aria-modal="true"]');
  return box&&!/вход|сообщени|авторизац/i.test(box.getAttribute('aria-label')||'');
 });
 if(modal.length===1){modal[0].focus();return {ok:true,ready:true,reason:'Редактор VK уже открыт'};}
 if(modal.length>1)return {ok:false,reason:'Открыто несколько полей редактора. Выбери нужное вручную.'};
 const candidates=[];
 for(const el of document.querySelectorAll('button,[role="button"],a,[tabindex],textarea,[contenteditable="true"],[contenteditable="plaintext-only"]')){
  if(!shown(el)||prohibited(el))continue;
  const attrs=[el.getAttribute('aria-label'),el.getAttribute('title'),el.getAttribute('data-placeholder'),el.getAttribute('placeholder'),el.getAttribute('data-testid')].filter(Boolean).join(' ').trim();
  const label=(attrs||el.textContent||'').replace(/\s+/g,' ').trim();
  if(!label||label.length>92)continue;
  const lower=label.toLowerCase();
  if(/опубликов|разместить|отправить|сохранить|коммент|ответить|предложить|репост|поделиться|истори|реклам|запланир|редактировать|закрепить|настроить/i.test(lower))continue;
  let score=0;
  if(/^(создать|добавить|написать|новая|новый)\s+(запись|записи|пост|публикацию|публикации)(?:\s+сообщества)?$/i.test(label))score=16;
  else if(/^(что\s+у\s+вас\s+нового|о\s+чём\s+хотите\s+рассказать|написать\s+что-нибудь)[?.…]*$/i.test(label))score=14;
  else if(/^(создать|написать|добавить).{0,20}(запис|пост|публикаци)/i.test(label))score=9;
  else continue;
  if(el.closest('[role="dialog"],dialog,[aria-modal="true"]'))continue;
  if(el.closest('main,[role="main"],#content'))score+=3;
  if(el.getBoundingClientRect().top<1000)score+=2;
  candidates.push({el,score,label:label.slice(0,65)});
 }
 candidates.sort((a,b)=>b.score-a.score);
 if(!candidates.length)return {ok:false,reason:'Кнопка «Создать запись» не найдена. Открой создание записи в VK вручную и нажми «Перенести текст и фото».'};
 if(candidates.length>1&&candidates[0].score===candidates[1].score)return {ok:false,reason:'Есть несколько одинаковых кнопок создания записи. Нужен ручной выбор.'};
 const chosen=candidates[0].el;
 chosen.scrollIntoView({block:'center',inline:'nearest'});
 chosen.focus();chosen.click();
 return {ok:true,clicked:true,reason:'Нажата кнопка создания записи: '+candidates[0].label};
})()`

// Confirm a post composer exists before touching its contents. Reject login,
// chat and comment editors, and refuse multiple plausible inputs.
const vkComposerReadyJS = `(function(){
 function visible(el){const r=el.getBoundingClientRect(),c=getComputedStyle(el);return r.width>90&&r.height>10&&c.display!=='none'&&c.visibility!=='hidden';}
 const els=[...document.querySelectorAll('textarea,[contenteditable="true"],[contenteditable="plaintext-only"]')].filter(el=>{
  if(!visible(el)||el.closest('header,nav,footer,[role="navigation"],[role="search"],aside,article,[data-post-id]'))return false;
  const name=[el.getAttribute('placeholder'),el.getAttribute('data-placeholder'),el.getAttribute('aria-label')].join(' ').toLowerCase();
  if(/поиск|комментари|сообщени|ответить|парол|email|телефон/.test(name))return false;
  const box=el.closest('[role="dialog"],dialog,[aria-modal="true"]');
  if(box&&/авторизац|вход|регистрац|сообщени/i.test(box.getAttribute('aria-label')||''))return false;
  return true;
 });
 const modal=els.filter(el=>!!el.closest('[role="dialog"],dialog,[aria-modal="true"]'));
 if(modal.length===1)return {ok:true};
 if(modal.length>1)return {ok:false,reason:'В окне VK больше одного поля записи'};
 const likely=els.filter(el=>/что.*нов|создать.*запис|напишите|текст запис|текст публикац|пост|публикац/i.test([el.getAttribute('placeholder'),el.getAttribute('data-placeholder'),el.getAttribute('aria-label')].join(' ')));
 if(likely.length===1 && els.length===1)return {ok:true};
 return {ok:false,reason:'Редактор записи не открылся'};
})()`

func cdpOpenVKComposer(c *cdpSocket) error {
	// The user may already have the actual composer open. Do not click
	// another create-post control and accidentally open a second draft.
	ready, readyErr := cdpEvaluate(c, vkComposerReadyJS)
	if readyErr == nil && ready["ok"] == true {
		return nil
	}
	click, err := cdpEvaluate(c, vkOpenComposerJS)
	if err != nil {
		return err
	}
	if click["ok"] != true {
		return fmt.Errorf("%v", click["reason"])
	}
	for i := 0; i < 32; i++ {
		ready, checkErr := cdpEvaluate(c, vkComposerReadyJS)
		if checkErr == nil && ready["ok"] == true {
			return nil
		}
		time.Sleep(180 * time.Millisecond)
	}
	return errors.New("VK не открыл редактор после нажатия. Создай запись вручную и используй проверенную кнопку переноса текста и фото")
}

// One deliberate click in News Agent prepares the handoff, opens its own VK
// browser window when absent, launches the composer and fills it. Neither this
// function nor its JS selectors ever confirms or schedules publication.
func (a *app) startVKPost(id string) (vkAutoFillResult, error) {
	vkAutoFillMu.Lock()
	defer vkAutoFillMu.Unlock()
	a.mu.Lock()
	it := a.getItemLocked(id)
	if it == nil {
		a.mu.Unlock()
		return vkAutoFillResult{}, errors.New("Новость не найдена")
	}
	if it.VKBrowserAutoPhotos && !it.VKBrowserAutoFillAt.IsZero() {
		a.mu.Unlock()
		return vkAutoFillResult{}, errors.New("Этот пост уже передавался в редактор VK. Проверь фотографии и не создавай дубль")
	}
	prepared := !it.VKBrowserPreparedAt.IsZero() && it.VKBrowserFingerprint == vkBrowserFingerprint(*it)
	community := a.state.Config.VKCommunityURL
	a.mu.Unlock()
	if !prepared {
		if _, err := a.prepareVKBrowser(id); err != nil {
			return vkAutoFillResult{}, err
		}
	}
	c, err := connectVKEditor(community)
	if err != nil {
		if err = openVKBrowserWindow(community); err != nil {
			return vkAutoFillResult{}, err
		}
		for i := 0; i < 45; i++ {
			time.Sleep(220 * time.Millisecond)
			c, err = connectVKEditor(community)
			if err == nil {
				break
			}
		}
		if err != nil {
			return vkAutoFillResult{}, errors.New("Окно VK открыто, но редактор ещё не готов. Дождись загрузки сообщества и повтори кнопку")
		}
	}
	err = cdpOpenVKComposer(c)
	c.close()
	if err != nil {
		return vkAutoFillResult{}, err
	}
	return a.autoFillVKLocked(id)
}

// ObjectId is not reused across pages. It is obtained after editor text insertion.
const vkFindImageInputJS = `(function(){
 // VK may create a hidden file input without an accept attribute or with a
 // single-file attribute. Prefer the editor's own input, never avatar/video.
 const ref=window.__mnaEditor;
 if(!ref || !document.contains(ref))return {ok:false,reason:'Поле записи изменилось. Кликни в текст поста и повтори.'};
 const modal=ref.closest('[role="dialog"],dialog,[aria-modal="true"]');
 const inputs=[...document.querySelectorAll('input[type="file"]')].filter(el=>!el.disabled);
 function score(el){
   if(modal && el.closest('[role="dialog"],dialog,[aria-modal="true"]')!==modal)return -1;
   const accept=(el.accept||'').toLowerCase();
   if(accept && /video|audio|pdf|\.doc|\.zip/.test(accept) && !/image|jpeg|jpg|png|webp/.test(accept))return -1;
   let score=0;
   if(/image|jpe?g|png|webp|\.jpe?g|\.png/.test(accept))score+=7;
   if(el.multiple)score+=4;
   const attrs=[el.id,el.name,el.getAttribute('aria-label'),el.getAttribute('data-testid'),el.parentElement?.getAttribute('aria-label'),el.parentElement?.textContent?.slice(0,80)].join(' ').toLowerCase();
   if(/фото|изображ|картин|photo|image|upload|attach|прикреп/.test(attrs))score+=5;
   if(modal&&modal.contains(el))score+=8;
   if(!modal){
     const a=ref.getBoundingClientRect(),b=el.getBoundingClientRect();
     if((a.width||0)&&(b.width||0)&&Math.abs(a.top-b.top)<500)score+=2;
   }
   return score;
 }
 const picks=inputs.map(el=>({el,score:score(el)})).filter(v=>v.score>=5).sort((a,b)=>b.score-a.score);
 if(!picks.length)return {ok:false,count:inputs.length,reason:'Поле фотографий ещё не открылось'};
 if(picks.length>1&&picks[0].score===picks[1].score)return {ok:false,count:picks.length,reason:'Несколько полей загрузки. Открой раздел «Фото» в редакторе VK и повтори.'};
 const el=picks[0].el;
 if(el.files?.length)return {ok:false,reason:'В VK уже выбраны фотографии. Не создаём дубликаты.'};
 if(!el.multiple)return {ok:false,reason:'Поле VK пока принимает только один файл. Попробуем кнопку добавления фото.',single:true};
 window.__mnaPhotoInput=el;
 return {ok:true,count:1};
})()`

// Finds a photo button close to the same composer whose text was filled.
// Never chooses a photo button in unrelated page navigation or account menus.
const vkFindPhotoButtonJS = `(function(){
 const ref=window.__mnaEditor;
 if(!ref||!document.contains(ref))return {ok:false,reason:'Сначала открой редактор записи VK'};
 const modal=ref.closest('[role="dialog"],dialog,[aria-modal="true"]');
 const rect=ref.getBoundingClientRect();
 function shown(el){const r=el.getBoundingClientRect(),cs=getComputedStyle(el);return r.width>12&&r.height>9&&cs.display!=='none'&&cs.visibility!=='hidden';}
 const candidates=[...document.querySelectorAll('button,[role="button"],label')].filter(el=>{
   if(el===window.__mnaLastPhotoButton)return false;
   if(!shown(el)||el.closest('header,nav,[role="navigation"],[role="search"]'))return false;
   if(modal&&el.closest('[role="dialog"],dialog,[aria-modal="true"]')!==modal)return false;
   return true;
 });
 const picks=[];
 for(const el of candidates){
   const desc=[el.getAttribute('aria-label'),el.getAttribute('title'),el.getAttribute('data-testid'),el.getAttribute('data-tooltip'),el.textContent?.trim().slice(0,65)].filter(Boolean).join(' ').toLowerCase();
   if(!/(фото|фотограф|изображени|картин|photo|image)/.test(desc)||/(удал|изменить|редакт|аватар|профил|обложк|delete|remove|avatar|cover)/.test(desc))continue;
   const r=el.getBoundingClientRect();
   const dx=Math.max(0,rect.left-r.right,r.left-rect.right),dy=Math.max(0,rect.top-r.bottom,r.top-rect.bottom);
   if(!modal&&(dx>400||dy>360))continue;
   let score=/(^|\s)(фото|фотографии|photo|photos)(\s|$)/.test(desc)?7:4;
   if(/добавить|загруз|прикреп|add|upload|attach/.test(desc))score+=3;
   if(modal)score+=6;
   score-=Math.min(5,Math.floor((dx+dy)/115));
   picks.push({el,score,desc:desc.slice(0,70)});
 }
 picks.sort((a,b)=>b.score-a.score);
 if(!picks.length)return {ok:false,reason:'Кнопка «Фото» не найдена. Нажми её в редакторе VK и повтори.'};
 if(picks.length>1&&picks[0].score===picks[1].score)return {ok:false,reason:'Несколько кнопок фото. Нажми нужную в редакторе VK и повтори.'};
 window.__mnaPhotoButton=picks[0].el;
 return {ok:true,reason:picks[0].desc};
})()`

type vkAutoFillResult struct {
	OK      bool   `json:"ok"`
	Text    bool   `json:"text"`
	Photos  bool   `json:"photos"`
	Details string `json:"details"`
	Images  int    `json:"images"`
}

func cdpEvaluate(c *cdpSocket, expr string) (map[string]any, error) {
	raw, err := c.call("Runtime.evaluate", map[string]any{"expression": expr, "returnByValue": true, "awaitPromise": false})
	if err != nil {
		return nil, err
	}
	var r struct {
		Result struct {
			Value map[string]any `json:"value"`
		} `json:"result"`
		ExceptionDetails any `json:"exceptionDetails"`
	}
	if err = json.Unmarshal(raw, &r); err != nil {
		return nil, err
	}
	if r.ExceptionDetails != nil {
		return nil, errors.New("Ошибка редактора VK при заполнении")
	}
	return r.Result.Value, nil
}
func cdpImageInputObject(c *cdpSocket) (string, error) {
	raw, err := c.call("Runtime.evaluate", map[string]any{"expression": "window.__mnaPhotoInput", "returnByValue": false})
	if err != nil {
		return "", err
	}
	var v struct {
		Result struct {
			ObjectID string `json:"objectId"`
		} `json:"result"`
	}
	if err = json.Unmarshal(raw, &v); err != nil {
		return "", err
	}
	if v.Result.ObjectID == "" {
		return "", errors.New("Не удалось выбрать поле фотографий VK")
	}
	return v.Result.ObjectID, nil
}
func (a *app) autoFillVK(id string) (vkAutoFillResult, error) {
	vkAutoFillMu.Lock()
	defer vkAutoFillMu.Unlock()
	return a.autoFillVKLocked(id)
}

func (a *app) autoFillVKLocked(id string) (vkAutoFillResult, error) {
	a.mu.Lock()
	it := a.getItemLocked(id)
	if it == nil {
		a.mu.Unlock()
		return vkAutoFillResult{}, errors.New("Новость не найдена")
	}
	if it.VKBrowserAutoPhotos && !it.VKBrowserAutoFillAt.IsZero() {
		a.mu.Unlock()
		return vkAutoFillResult{}, errors.New("Фотографии уже передавались в VK. Проверь редактор, повторная загрузка отключена против дублей. Если нужно повторить, заново подготовь пакет")
	}
	if it.State != "approved" || it.VKPostID != 0 || it.VKBrowserPreparedAt.IsZero() || vkBrowserFingerprint(*it) != it.VKBrowserFingerprint {
		a.mu.Unlock()
		return vkAutoFillResult{}, errors.New("Сначала подготовь актуальный одобренный пост; не создавай дубликаты")
	}
	folder := it.VKBrowserExportDir
	body := it.Draft
	n := len(it.Images)
	community := a.state.Config.VKCommunityURL
	a.mu.Unlock()
	base := filepath.Join(a.dataDir, "VK_Export")
	rel, err := filepath.Rel(base, folder)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return vkAutoFillResult{}, errors.New("Недопустимая папка подготовки VK")
	}
	entries, err := os.ReadDir(folder)
	if err != nil {
		return vkAutoFillResult{}, err
	}
	paths := make([]string, n)
	for i := 1; i <= n; i++ {
		prefix := fmt.Sprintf("%02d.", i)
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasPrefix(entry.Name(), prefix) && (strings.HasSuffix(strings.ToLower(entry.Name()), ".png") || strings.HasSuffix(strings.ToLower(entry.Name()), ".jpg") || strings.HasSuffix(strings.ToLower(entry.Name()), ".jpeg")) {
				paths[i-1] = filepath.Join(folder, entry.Name())
				break
			}
		}
		if paths[i-1] == "" {
			return vkAutoFillResult{}, fmt.Errorf("Не найдена подготовленная фотография %02d", i)
		}
	}
	c, err := connectVKEditor(community)
	if err != nil {
		return vkAutoFillResult{}, err
	}
	defer c.close()
	b, _ := json.Marshal(body)
	txt, err := cdpEvaluate(c, strings.Replace(vkFillTextJS, "__TEXT__", string(b), 1))
	if err != nil {
		return vkAutoFillResult{}, err
	}
	if txt["ok"] != true {
		return vkAutoFillResult{OK: false, Details: fmt.Sprint(txt["reason"])}, nil
	}
	ret := vkAutoFillResult{Text: true, Details: "Текст перенесён. "}
	method, err := cdpAttachVKPhotos(c, paths)
	if err != nil {
		ret.Details += "Фото не переданы: " + err.Error() + ". Можно перетащить их из подготовленной папки."
		return ret, nil
	}
	ret.Photos = true
	ret.OK = true
	ret.Images = n
	ret.Details = fmt.Sprintf("Текст вставлен. В Edge выбрано %d фото (%s). Дождись появления всех миниатюр в VK. Загрузка на сервер VK не проверена. Время и публикацию подтверждаешь вручную.", n, method)
	a.mu.Lock()
	current := a.getItemLocked(id)
	if current != nil && current.State == "approved" && current.VKBrowserFingerprint == vkBrowserFingerprint(*current) {
		current.VKBrowserAutoFillAt = time.Now().UTC()
		current.VKBrowserAutoPhotos = true
		a.auditLocked("vk_browser_auto_fill", id, "Выбор файлов в редакторе Edge; реальное завершение загрузки в VK не подтверждено")
		if err := a.persistLocked(); err != nil {
			a.mu.Unlock()
			return ret, fmt.Errorf("Файлы переданы, но не удалось сохранить статус: %w", err)
		}
	}
	a.mu.Unlock()
	return ret, nil
}
