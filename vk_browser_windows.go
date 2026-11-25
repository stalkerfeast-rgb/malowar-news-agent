//go:build windows

package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
)

// Official VK site in a dedicated Microsoft Edge app window with persistent
// user data. The explicit Auto-fill action uses local CDP. No VK API
// token, login automation or publication confirmation is performed.
func openVKBrowserWindow(communityURL string) error {
	paths := []string{}
	for _, key := range []string{"PROGRAMFILES(X86)", "PROGRAMFILES", "LOCALAPPDATA"} {
		if base := os.Getenv(key); base != "" {
			paths = append(paths, filepath.Join(base, "Microsoft", "Edge", "Application", "msedge.exe"))
		}
	}
	var edge string
	for _, path := range paths {
		if st, err := os.Stat(path); err == nil && !st.IsDir() {
			edge = path
			break
		}
	}
	if edge == "" {
		return errors.New("Не найден Microsoft Edge. Установи Edge для постоянного браузерного профиля VK")
	}
	root := os.Getenv("LOCALAPPDATA")
	if root == "" {
		return errors.New("Windows не предоставила LOCALAPPDATA для профиля браузера")
	}
	profile := filepath.Join(root, "MalowarNewsAgent", "VKBrowser")
	if err := os.MkdirAll(profile, 0700); err != nil {
		return err
	}
	cmd := exec.Command(edge, "--app="+communityURL, "--user-data-dir="+profile, "--no-first-run", "--remote-debugging-port=0", "--remote-debugging-address=127.0.0.1")
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
func revealVKFolder(path string) error {
	cmd := exec.Command("explorer.exe", path)
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

func vkBrowserProfilePath() string {
	return filepath.Join(os.Getenv("LOCALAPPDATA"), "MalowarNewsAgent", "VKBrowser")
}
