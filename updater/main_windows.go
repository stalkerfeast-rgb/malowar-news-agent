//go:build windows

package main

import (
	"archive/zip"
	"crypto/sha256"
	"debug/pe"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

const agentName = "Malowar_News_Agent.exe"

func message(text string) {
	u := syscall.NewLazyDLL("user32.dll").NewProc("MessageBoxW")
	title, _ := syscall.UTF16PtrFromString("Malowar News Agent — обновление")
	body, _ := syscall.UTF16PtrFromString(text)
	u.Call(0, uintptr(unsafe.Pointer(body)), uintptr(unsafe.Pointer(title)), 0x10)
}
func copyFile(src, dest string) error {
	input, e := os.Open(src)
	if e != nil {
		return e
	}
	defer input.Close()
	out, e := os.Create(dest)
	if e != nil {
		return e
	}
	if _, e = io.Copy(out, input); e != nil {
		out.Close()
		return e
	}
	return out.Close()
}
func isWithin(root, path string) bool {
	rel, e := filepath.Rel(root, path)
	return e == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
func run(dir, pkg, want, version string) error {
	dir, e := filepath.Abs(filepath.Clean(dir))
	if e != nil {
		return e
	}
	exe, e := os.Executable()
	if e != nil {
		return e
	}
	if !strings.EqualFold(filepath.Dir(exe), dir) {
		return errors.New("Обновлятор запущен не из папки приложения")
	}
	if !isWithin(filepath.Join(dir, "Data", "updates"), pkg) {
		return errors.New("Архив не находится в защищённом каталоге обновлений")
	}
	marker := filepath.Join(dir, ".mna-install.json")
	raw, e := os.ReadFile(marker)
	if e != nil {
		return e
	}
	var meta struct {
		ID        string `json:"id"`
		Directory string `json:"directory"`
		Version   string `json:"version"`
	}
	if e = json.Unmarshal(raw, &meta); e != nil {
		return e
	}
	if meta.ID != "MalowarNewsAgent" || !strings.EqualFold(filepath.Clean(meta.Directory), dir) {
		return errors.New("Не подтверждена папка установки")
	}
	if len(want) != 64 || len(version) < 5 || len(version) > 24 {
		return errors.New("Некорректный пакет обновления")
	}
	f, e := os.Open(pkg)
	if e != nil {
		return e
	}
	h := sha256.New()
	_, e = io.Copy(h, io.LimitReader(f, 80<<20+1))
	f.Close()
	if e != nil {
		return e
	}
	if hex.EncodeToString(h.Sum(nil)) != strings.ToLower(want) {
		return errors.New("Контрольная сумма архива не совпадает")
	}
	archive, e := zip.OpenReader(pkg)
	if e != nil {
		return e
	}
	defer archive.Close()
	var bin *zip.File
	for _, it := range archive.File {
		if it.Name == agentName {
			bin = it
		}
	}
	if bin == nil || bin.UncompressedSize64 == 0 || bin.UncompressedSize64 > 70<<20 {
		return errors.New("В архиве нет правильного исполняемого файла")
	}
	staging := filepath.Join(dir, ".mna-agent-next.exe")
	_ = os.Remove(staging)
	in, e := bin.Open()
	if e != nil {
		return e
	}
	out, e := os.OpenFile(staging, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if e != nil {
		in.Close()
		return e
	}
	_, e = io.Copy(out, io.LimitReader(in, 70<<20+1))
	in.Close()
	if closeErr := out.Close(); e == nil {
		e = closeErr
	}
	if e != nil {
		_ = os.Remove(staging)
		return e
	}
	defer os.Remove(staging)
	p, e := pe.Open(staging)
	if e != nil {
		return fmt.Errorf("Загруженный файл не является Windows EXE: %w", e)
	}
	if p.FileHeader.Machine != pe.IMAGE_FILE_MACHINE_AMD64 {
		p.Close()
		return errors.New("Неверная архитектура обновления")
	}
	p.Close()
	backupDir := filepath.Join(dir, "Data", "updates", "backups", time.Now().Format("20060102-150405"))
	if e = os.MkdirAll(backupDir, 0700); e != nil {
		return e
	}
	for _, name := range []string{"state.json", "state.json.bak"} {
		src := filepath.Join(dir, "Data", name)
		if _, err := os.Stat(src); err == nil {
			if err = copyFile(src, filepath.Join(backupDir, name)); err != nil {
				return fmt.Errorf("Не удалось сохранить данные: %w", err)
			}
		}
	}
	existing := filepath.Join(dir, agentName)
	backupExe := filepath.Join(backupDir, agentName)
	// Allow the parent GUI process to shut down before replacing its executable.
	var last error
	for i := 0; i < 150; i++ {
		last = os.Rename(existing, backupExe)
		if last == nil {
			break
		}
		time.Sleep(400 * time.Millisecond)
	}
	if last != nil {
		return fmt.Errorf("Закрой News Agent перед обновлением: %w", last)
	}
	if e = os.Rename(staging, existing); e != nil {
		_ = os.Rename(backupExe, existing)
		return fmt.Errorf("Не удалось установить новую версию: %w", e)
	}
	var newMeta map[string]any
	if json.Unmarshal(raw, &newMeta) == nil {
		newMeta["version"] = version
		if next, err := json.MarshalIndent(newMeta, "", "  "); err == nil {
			_ = os.WriteFile(marker, next, 0600)
		}
	}
	cmd := exec.Command(existing)
	cmd.Dir = dir
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if e = cmd.Start(); e != nil {
		_ = os.Remove(existing)
		_ = os.Rename(backupExe, existing)
		_ = os.WriteFile(marker, raw, 0600)
		return fmt.Errorf("Не удалось запустить новую версию, старый EXE восстановлен: %w", e)
	}
	_ = cmd.Process.Release()
	// Maintain the Windows uninstall-list version after a successful launch.
	_ = exec.Command("reg.exe", "ADD", `HKCU\Software\Microsoft\Windows\CurrentVersion\Uninstall\MalowarNewsAgent`, "/v", "DisplayVersion", "/t", "REG_SZ", "/d", version, "/f").Run()
	return nil
}
func main() {
	dir := flag.String("dir", "", "")
	pkg := flag.String("zip", "", "")
	hash := flag.String("sha256", "", "")
	ver := flag.String("version", "", "")
	flag.Parse()
	if err := run(*dir, *pkg, *hash, *ver); err != nil {
		d := filepath.Join(*dir, "Data", "updates")
		_ = os.MkdirAll(d, 0700)
		_ = os.WriteFile(filepath.Join(d, "update-error.txt"), []byte(err.Error()), 0600)
		message("Обновление не установлено:\n" + err.Error())
	}
}
