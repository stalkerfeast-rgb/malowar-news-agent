//go:build !windows

package main

import (
	"os/exec"
)

func openAppWindow(url string) { _ = exec.Command("xdg-open", url).Start() }

func startAppTray(_ string, _ func()) func() { return func() {} }
