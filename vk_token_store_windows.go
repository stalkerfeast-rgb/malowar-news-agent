//go:build windows

package main

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"unsafe"
)

// Windows DPAPI encrypts for the current Windows profile; no passwords or raw keys
// are embedded in News Agent, its JSON data, updates, or GitHub releases.
type vkDataBlob struct {
	Size uint32
	Data *byte
}

var (
	vkCrypt32    = syscall.NewLazyDLL("crypt32.dll")
	vkProtect    = vkCrypt32.NewProc("CryptProtectData")
	vkUnprotect  = vkCrypt32.NewProc("CryptUnprotectData")
	vkKernel     = syscall.NewLazyDLL("kernel32.dll")
	vkLocalFree  = vkKernel.NewProc("LocalFree")
	vkMoveFileEx = vkKernel.NewProc("MoveFileExW")
)

func vkVaultPath() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	if base == "" {
		return "", errors.New("папка профиля Windows недоступна")
	}
	return filepath.Join(base, "MalowarNewsAgent", "Secrets", "vk-token.dpapi"), nil
}

func vkCrypt(data []byte, protect bool) ([]byte, error) {
	if len(data) == 0 {
		return nil, errors.New("пустые данные DPAPI")
	}
	input := vkDataBlob{Size: uint32(len(data)), Data: &data[0]}
	var output vkDataBlob
	proc := vkProtect
	if !protect {
		proc = vkUnprotect
	}
	ret, _, callErr := proc.Call(
		uintptr(unsafe.Pointer(&input)), 0, 0, 0, 0,
		uintptr(1), // CRYPTPROTECT_UI_FORBIDDEN
		uintptr(unsafe.Pointer(&output)),
	)
	runtime.KeepAlive(data)
	if output.Data != nil {
		defer vkLocalFree.Call(uintptr(unsafe.Pointer(output.Data)))
	}
	if ret == 0 {
		return nil, callErr
	}
	if output.Data == nil || output.Size == 0 || output.Size > 64*1024 {
		return nil, errors.New("некорректный ответ DPAPI")
	}
	b := make([]byte, int(output.Size))
	copy(b, unsafe.Slice(output.Data, int(output.Size)))
	return b, nil
}

func readVKCredential(path string) (string, error) {
	encrypted, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if len(encrypted) > 65536 || len(encrypted) == 0 {
		return "", errors.New("некорректный файл ключа")
	}
	plain, err := vkCrypt(encrypted, false)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

func writeVKCredential(path, token string) error {
	if token == "" {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	encrypted, err := vkCrypt([]byte(token), true)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".vk-token-*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err = f.Chmod(0600); err != nil {
		f.Close()
		return err
	}
	if _, err = f.Write(encrypted); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	src, err := syscall.UTF16PtrFromString(tmp)
	if err != nil {
		return err
	}
	dst, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	// MOVEFILE_REPLACE_EXISTING | MOVEFILE_WRITE_THROUGH; atomic replacement.
	ok, _, e := vkMoveFileEx.Call(uintptr(unsafe.Pointer(src)), uintptr(unsafe.Pointer(dst)), uintptr(1|8))
	if ok == 0 {
		return e
	}
	return nil
}
