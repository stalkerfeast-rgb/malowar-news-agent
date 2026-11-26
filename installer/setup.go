//go:build windows && mna_setup

package main

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

//go:embed payload/Malowar_News_Agent.exe payload/Malowar_News_Uninstall.exe payload/Malowar_News_Updater.exe payload/Malowar_News_Agent.ico
var payload embed.FS

type setupChoice struct {
	Path    string `json:"path"`
	Desktop bool   `json:"desktop"`
	Start   bool   `json:"start"`
}

func setup() error {
	root := os.Getenv("LOCALAPPDATA")
	if root == "" {
		return errors.New("Нет LOCALAPPDATA")
	}
	defaultDir := filepath.Join(root, "Programs", "Malowar News Agent")
	if info, err := os.Stat(`E:\`); err == nil && info.IsDir() {
		defaultDir = `E:\Агент`
	}
	result, err := runDialog(setupDialog, "mna-setup-", "-DefaultDir", defaultDir)
	if err != nil {
		return err
	}
	if result == nil {
		return nil
	}
	var c setupChoice
	if err := json.Unmarshal(result, &c); err != nil {
		return err
	}
	dir, err := validateTarget(c.Path)
	if err != nil {
		return err
	}
	// Before an in-place upgrade, preserve the editorial database. Image files
	// and other user data are never replaced by the installer.
	if verifyMarker(dir) == nil {
		backupDir := filepath.Join(dir, "Data", "updates", "backups", "setup-"+time.Now().Format("20060102-150405"))
		for _, name := range []string{"state.json", "state.json.bak"} {
			src := filepath.Join(dir, "Data", name)
			if _, statErr := os.Stat(src); statErr == nil {
				if mkErr := os.MkdirAll(backupDir, 0700); mkErr != nil {
					return mkErr
				}
				if copyErr := copyFile(src, filepath.Join(backupDir, name)); copyErr != nil {
					return fmt.Errorf("Не удалось создать резервную копию %s: %w", name, copyErr)
				}
			}
		}
	}
	exe, err := payload.ReadFile("payload/Malowar_News_Agent.exe")
	if err != nil {
		return err
	}
	undo, err := payload.ReadFile("payload/Malowar_News_Uninstall.exe")
	if err != nil {
		return err
	}
	if err = writeFile(filepath.Join(dir, "Malowar_News_Agent.exe"), exe); err != nil {
		return err
	}
	if err = writeFile(filepath.Join(dir, "Malowar_News_Uninstall.exe"), undo); err != nil {
		return err
	}
	updater, err := payload.ReadFile("payload/Malowar_News_Updater.exe")
	if err != nil {
		return err
	}
	if err = writeFile(filepath.Join(dir, "Malowar_News_Updater.exe"), updater); err != nil {
		return err
	}
	icon, err := payload.ReadFile("payload/Malowar_News_Agent.ico")
	if err != nil {
		return err
	}
	if err = writeFile(filepath.Join(dir, "Malowar_News_Agent.ico"), icon); err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Join(dir, "Data"), 0700); err != nil {
		return err
	}
	if err = writeMarker(dir); err != nil {
		return err
	}
	if err = registerUninstaller(dir); err != nil {
		return fmt.Errorf("Регистрация: %w", err)
	}
	if err = createShortcuts(dir, c.Desktop); err != nil {
		return fmt.Errorf("Ярлыки: %w", err)
	}
	msgBox("Malowar News Agent", "Установка завершена.\n\nПапка: "+dir+"\n\nУдаление доступно через список установленных программ Windows.\n\nНикакие новости в VK автоматически не публикуются без твоего одобрения.", 0x40)
	if c.Start {
		cmd := exec.Command(filepath.Join(dir, "Malowar_News_Agent.exe"))
		cmd.Dir = dir
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		if cmd.Start() == nil {
			_ = cmd.Process.Release()
		}
	}
	return nil
}
func main() {
	if err := setup(); err != nil {
		errorBox(err)
	}
}
