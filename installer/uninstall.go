//go:build windows && mna_uninstall

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type uninstallChoice struct {
	RemoveData bool `json:"removeData"`
}

func isRunning(dir string) bool {
	b, err := os.ReadFile(filepath.Join(dir, "Data", "active.json"))
	if err != nil {
		return false
	}
	var state struct {
		URL string `json:"url"`
	}
	if json.Unmarshal(b, &state) != nil {
		return false
	}
	if !strings.HasPrefix(state.URL, "http://127.0.0.1:") {
		return false
	}
	cl := http.Client{Timeout: 1500 * time.Millisecond}
	resp, err := cl.Get(state.URL)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return strings.HasPrefix(resp.Header.Get("X-MNA-Agent"), "v1.")
}
func uninstall() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	dir := filepath.Dir(exe)
	if err = verifyMarker(dir); err != nil {
		return fmt.Errorf("Не подтверждена папка приложения: %w", err)
	}
	if isRunning(dir) {
		return errors.New("Сначала закрой Malowar News Agent кнопкой «Выход», затем повтори удаление")
	}
	result, err := runDialog(uninstallDialog, "mna-uninstall-", "-InstallDir", dir)
	if err != nil {
		return err
	}
	if result == nil {
		return nil
	}
	var c uninstallChoice
	if err = json.Unmarshal(result, &c); err != nil {
		return err
	}
	tmp, err := os.CreateTemp("", "mna-cleanup-*.exe")
	if err != nil {
		return err
	}
	name := tmp.Name()
	_ = tmp.Close()
	if err = copyFile(exe, name); err != nil {
		_ = os.Remove(name)
		return err
	}
	cmd := exec.Command(name, "--cleanup", dir, strconv.FormatBool(c.RemoveData))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err = cmd.Start(); err != nil {
		_ = os.Remove(name)
		return err
	}
	return cmd.Process.Release()
}
func cleanup(dir string, removeData bool) error {
	if err := verifyMarker(dir); err != nil {
		return err
	}
	if isRunning(dir) {
		return errors.New("Программа ещё работает. Закрой её кнопкой Выход")
	}
	problems := []string{}
	if err := removeShortcuts(); err != nil {
		problems = append(problems, "Ярлыки: "+err.Error())
	}
	if err := unregister(); err != nil {
		problems = append(problems, "Реестр: "+err.Error())
	}
	if removeData {
		if err := os.RemoveAll(filepath.Join(dir, "Data")); err != nil {
			problems = append(problems, "Данные: "+err.Error())
		}
	}
	for _, name := range []string{"Malowar_News_Agent.exe", "Malowar_News_Updater.exe", "Malowar_News_Uninstall.exe", markerName} {
		path := filepath.Join(dir, name)
		for i := 0; i < 20; i++ {
			err := os.Remove(path)
			if err == nil || os.IsNotExist(err) {
				break
			}
			if i == 19 {
				problems = append(problems, name+": "+err.Error())
			}
			time.Sleep(200 * time.Millisecond)
		}
	}
	_ = os.Remove(dir)
	if len(problems) > 0 {
		return errors.New("Не удалось удалить все файлы. Убедись, что программа закрыта:\n" + strings.Join(problems, "\n"))
	}
	msg := "Программа удалена. Черновики и история сохранены в:\n" + filepath.Join(dir, "Data")
	if removeData {
		msg = "Программа и локальная редакционная база удалены."
	}
	msgBox("Malowar News Agent", msg, 0x40)
	return nil
}
func main() {
	if len(os.Args) == 4 && os.Args[1] == "--cleanup" {
		flag, _ := strconv.ParseBool(os.Args[3])
		if err := cleanup(os.Args[2], flag); err != nil {
			errorBox(err)
		}
		return
	}
	if err := uninstall(); err != nil {
		errorBox(err)
	}
}
