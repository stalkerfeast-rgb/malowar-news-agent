//go:build windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
)

func openAppWindow(url string) {
	paths := []string{}
	if x := os.Getenv("PROGRAMFILES(X86)"); x != "" {
		paths = append(paths, filepath.Join(x, "Microsoft", "Edge", "Application", "msedge.exe"))
	}
	if x := os.Getenv("PROGRAMFILES"); x != "" {
		paths = append(paths, filepath.Join(x, "Microsoft", "Edge", "Application", "msedge.exe"))
	}
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			c := exec.Command(p, "--app="+url, "--no-first-run")
			if c.Start() == nil {
				_ = c.Process.Release()
				return
			}
		}
	}
	c := exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", url)
	_ = c.Start()
}
