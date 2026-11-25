//go:build !windows

package main

// On non-Windows, tests and development runs retain the old RAM-only behaviour.
func vkVaultPath() (string, error)                 { return "", nil }
func readVKCredential(path string) (string, error) { return "", nil }
func writeVKCredential(path, token string) error   { return nil }
