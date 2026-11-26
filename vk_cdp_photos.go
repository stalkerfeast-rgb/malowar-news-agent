package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// The file chooser belongs to the VK page in the dedicated Edge profile.
// Interception is always disabled before returning, preserving normal manual
// browser uploads. No wall/post button is clicked by this integration.
func (c *cdpSocket) waitEvent(method string, timeout time.Duration) (json.RawMessage, error) {
	until := time.Now().Add(timeout)
	for {
		for i, e := range c.events {
			if e.Method == method {
				c.events = append(c.events[:i], c.events[i+1:]...)
				return e.Params, nil
			}
		}
		if time.Now().After(until) {
			return nil, fmt.Errorf("событие %s не появилось", method)
		}
		_ = c.conn.SetReadDeadline(until)
		op, raw, err := c.readFrame()
		if err != nil {
			return nil, err
		}
		if op == 9 {
			if err := c.sendFrame(10, raw); err != nil {
				return nil, err
			}
			continue
		}
		if op == 8 {
			return nil, errors.New("Окно Edge закрыло соединение")
		}
		if op != 1 && op != 2 {
			continue
		}
		var e cdpEvent
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, err
		}
		if e.Method == method {
			return e.Params, nil
		}
		if e.Method != "" && len(c.events) < 24 {
			c.events = append(c.events, e)
		}
	}
}

func cdpEvaluateGesture(c *cdpSocket, script string) (map[string]any, error) {
	raw, err := c.call("Runtime.evaluate", map[string]any{
		"expression": script, "returnByValue": true, "awaitPromise": false, "userGesture": true,
	})
	if err != nil {
		return nil, err
	}
	var r struct {
		Result struct {
			Value map[string]any `json:"value"`
		} `json:"result"`
		ExceptionDetails any `json:"exceptionDetails"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, err
	}
	if r.ExceptionDetails != nil {
		return nil, errors.New("VK не разрешил нажать кнопку добавления фото")
	}
	return r.Result.Value, nil
}

func cdpAttachDirect(c *cdpSocket, files []string) error {
	obj, err := cdpImageInputObject(c)
	if err != nil {
		return err
	}
	_, err = c.call("DOM.setFileInputFiles", map[string]any{"files": files, "objectId": obj})
	if err != nil {
		return fmt.Errorf("не удалось выбрать фотографии: %w", err)
	}
	return nil
}

func cdpAttachChooser(c *cdpSocket, files []string, event json.RawMessage) error {
	var e struct {
		BackendNodeID int    `json:"backendNodeId"`
		Mode          string `json:"mode"`
	}
	if err := json.Unmarshal(event, &e); err != nil {
		return err
	}
	if e.BackendNodeID <= 0 {
		return errors.New("Edge не предоставил поле выбора файлов")
	}
	if e.Mode == "selectSingle" && len(files) > 1 {
		return errors.New("VK открыл окно для одной фотографии, а у записи их несколько")
	}
	_, err := c.call("DOM.setFileInputFiles", map[string]any{"files": files, "backendNodeId": e.BackendNodeID})
	if err != nil {
		return fmt.Errorf("Edge отказался выбирать фотографии: %w", err)
	}
	return nil
}

func cdpPhotoInput(c *cdpSocket, files []string) (bool, error) {
	r, err := cdpEvaluate(c, vkFindImageInputJS)
	if err != nil {
		return false, err
	}
	if r["ok"] == true {
		return true, cdpAttachDirect(c, files)
	}
	reason := fmt.Sprint(r["reason"])
	if strings.Contains(reason, "уже выбраны") || strings.Contains(reason, "Несколько полей") || strings.Contains(reason, "Поле записи изменилось") {
		return false, errors.New(reason)
	}
	return false, nil
}

func cdpAttachVKPhotos(c *cdpSocket, files []string) (string, error) {
	if len(files) < 1 || len(files) > 7 {
		return "", errors.New("Нужно от 1 до 7 фотографий")
	}
	if found, err := cdpPhotoInput(c, files); found || err != nil {
		if err != nil {
			return "", err
		}
		return "поле фотографий VK", nil
	}

	// This catches file inputs created on demand by React/VK when clicking
	// the official photo attachment control, even without accept/multiple.
	if _, err := c.call("Page.enable", map[string]any{}); err != nil {
		return "", err
	}
	if _, err := c.call("Page.setInterceptFileChooserDialog", map[string]any{"enabled": true}); err != nil {
		return "", fmt.Errorf("Edge не поддерживает перехват окна выбора фото: %w", err)
	}
	defer func() { _, _ = c.call("Page.setInterceptFileChooserDialog", map[string]any{"enabled": false}) }()

	for attempt := 0; attempt < 2; attempt++ {
		found, err := cdpEvaluate(c, vkFindPhotoButtonJS)
		if err != nil {
			return "", err
		}
		if found["ok"] != true {
			if err := cdpDragVKPhotos(c, files); err == nil {
				return "безопасное перетаскивание", nil
			}
			return "", errors.New(fmt.Sprint(found["reason"]) + "; перетаскивание не подтверждено")
		}
		// The click is limited to a photo control in the active composer.
		// It can open a menu or a file chooser, never submit or publish.
		click, err := cdpEvaluateGesture(c, `(function(){
          const el=window.__mnaPhotoButton;
          if(!el||!document.contains(el))return {ok:false,reason:'Кнопка фото исчезла'};
          window.__mnaLastPhotoButton=el;
          el.click();
          return {ok:true};
        })()`)
		if err != nil {
			return "", err
		}
		if click["ok"] != true {
			return "", errors.New(fmt.Sprint(click["reason"]))
		}
		event, err := c.waitEvent("Page.fileChooserOpened", 850*time.Millisecond)
		if err == nil {
			if err := cdpAttachChooser(c, files, event); err != nil {
				// Some VK layouts have a single-selection chooser but still
				// support the already familiar multi-file drag-and-drop.
				if strings.Contains(err.Error(), "одной фотографии") {
					if dragErr := cdpDragVKPhotos(c, files); dragErr == nil {
						return "безопасное перетаскивание", nil
					}
				}
				return "", err
			}
			return "кнопка «Фото»", nil
		}
		// Some VK versions expand a menu or mount the input after a click.
		// Try the direct input before looking for the second menu button.
		if found, err := cdpPhotoInput(c, files); found || err != nil {
			if err != nil {
				return "", err
			}
			return "открывшееся поле VK", nil
		}
	}
	if err := cdpDragVKPhotos(c, files); err == nil {
		return "безопасное перетаскивание", nil
	}
	return "", errors.New("VK не открыл поле загрузки и не подтвердил перетаскивание. Нажми «Фото» и повтори или перетащи файлы вручную")
}

// cdpDragVKPhotos is a last resort for a composer supporting the exact drag
// and drop workflow that the user already performs manually. It MUST NOT drop
// into a page that did not prevent the browser's default dragover action:
// doing so could navigate away from an unsaved VK draft.
func cdpDragVKPhotos(c *cdpSocket, files []string) error {
	point, err := cdpEvaluate(c, `(function(){
      const el=window.__mnaEditor;
      if(!el||!document.contains(el))return {ok:false};
      const r=el.getBoundingClientRect();
      const x=Math.round(r.left+r.width/2), y=Math.round(r.top+r.height/2);
      if(r.width<95||r.height<12||x<0||y<0||x>=innerWidth||y>=innerHeight)return {ok:false};
      window.__mnaDropCheck={allowed:false,seen:false,count:0};
      if(window.__mnaDragOverListener)window.removeEventListener('dragover',window.__mnaDragOverListener);
      if(window.__mnaDropListener)window.removeEventListener('drop',window.__mnaDropListener);
      window.__mnaDragOverListener=(e)=>{window.__mnaDropCheck.allowed=window.__mnaDropCheck.allowed||e.defaultPrevented;};
      window.__mnaDropListener=(e)=>{window.__mnaDropCheck.seen=true;window.__mnaDropCheck.count=e.dataTransfer?.files?.length||0;};
      window.addEventListener('dragover',window.__mnaDragOverListener,false);
      window.addEventListener('drop',window.__mnaDropListener,true);
      return {ok:true,x:x,y:y};
    })()`)
	if err != nil {
		return err
	}
	if point["ok"] != true {
		return errors.New("Не удалось определить безопасную область для переноса фотографий")
	}
	x, okX := point["x"].(float64)
	y, okY := point["y"].(float64)
	if !okX || !okY {
		return errors.New("Координаты редактора VK неизвестны")
	}
	data := map[string]any{"items": []any{}, "files": files, "dragOperationsMask": 1}
	drag := func(kind string) error {
		_, e := c.call("Input.dispatchDragEvent", map[string]any{"type": kind, "x": x, "y": y, "data": data})
		return e
	}
	defer func() {
		_, _ = cdpEvaluate(c, `(function(){
      if(window.__mnaDragOverListener)window.removeEventListener('dragover',window.__mnaDragOverListener);
      if(window.__mnaDropListener)window.removeEventListener('drop',window.__mnaDropListener);
      return {ok:true};
    })()`)
	}()
	if err := drag("dragEnter"); err != nil {
		return err
	}
	if err := drag("dragOver"); err != nil {
		_ = drag("dragCancel")
		return err
	}
	check, err := cdpEvaluate(c, "({allowed:!!window.__mnaDropCheck?.allowed})")
	if err != nil || check["allowed"] != true {
		_ = drag("dragCancel")
		return errors.New("VK не подтвердил приём перетаскивания в редакторе; автоматический сброс отменён, чтобы не потерять черновик")
	}
	if err := drag("drop"); err != nil {
		return err
	}
	check, err = cdpEvaluate(c, "({seen:!!window.__mnaDropCheck?.seen,count:window.__mnaDropCheck?.count||0})")
	if err != nil {
		return err
	}
	if check["seen"] != true || check["count"] != float64(len(files)) {
		return errors.New("Перенос не подтверждён браузером. Проверь, не появились ли фото, прежде чем повторять")
	}
	return nil
}
