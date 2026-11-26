//go:build windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

// Starts Ollama only after the user explicitly enables local AI.
// It deliberately does not install services or modify startup settings.
func tryStartOllama() bool {
	local := os.Getenv("LOCALAPPDATA")
	if local != "" {
		gui := filepath.Join(local, "Programs", "Ollama", "ollama app.exe")
		if fi, err := os.Stat(gui); err == nil && !fi.IsDir() {
			cmd := exec.Command(gui)
			cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
			if cmd.Start() == nil {
				_ = cmd.Process.Release()
				return true
			}
		}
	}
	cli, err := exec.LookPath("ollama.exe")
	if err != nil && local != "" {
		cli = filepath.Join(local, "Programs", "Ollama", "ollama.exe")
	}
	if fi, err := os.Stat(cli); err != nil || fi.IsDir() {
		return false
	}
	cmd := exec.Command(cli, "serve")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if cmd.Start() != nil {
		return false
	}
	_ = cmd.Process.Release()
	return true
}
