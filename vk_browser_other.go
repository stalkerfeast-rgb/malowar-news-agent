//go:build !windows

package main

import "errors"

func openVKBrowserWindow(communityURL string) error {
	return errors.New("Окно VK поддерживается только в Windows с Microsoft Edge")
}
func revealVKFolder(path string) error {
	return errors.New("Открытие папки доступно в Windows")
}
func vkBrowserProfilePath() string { return "" }
