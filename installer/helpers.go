//go:build windows

package main

import (
	"encoding/json"
	"errors"
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

const appID = "MalowarNewsAgent"
const appVersion = "1.3.19"
const markerName = ".mna-install.json"
const uninstallKey = `HKCU\Software\Microsoft\Windows\CurrentVersion\Uninstall\MalowarNewsAgent`

func msgBox(title, msg string, flags uintptr) uintptr {
	u := syscall.NewLazyDLL("user32.dll").NewProc("MessageBoxW")
	t, _ := syscall.UTF16PtrFromString(title)
	m, _ := syscall.UTF16PtrFromString(msg)
	r, _, _ := u.Call(0, uintptr(unsafe.Pointer(m)), uintptr(unsafe.Pointer(t)), flags)
	return r
}

func errorBox(err error) {
	if err != nil {
		msgBox("Malowar News Agent", err.Error(), 0x10)
	}
}

func powershell() string {
	root := os.Getenv("SystemRoot")
	full := filepath.Join(root, "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	if _, err := os.Stat(full); err == nil {
		return full
	}
	return "powershell.exe"
}

func runDialog(script, prefix string, args ...string) ([]byte, error) {
	tempDir, err := os.MkdirTemp("", prefix)
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tempDir)
	scriptFile := filepath.Join(tempDir, "dialog.ps1")
	resultFile := filepath.Join(tempDir, "choice.json")
	if err := os.WriteFile(scriptFile, append([]byte{0xef, 0xbb, 0xbf}, []byte(script)...), 0600); err != nil {
		return nil, err
	}
	cmdArgs := []string{"-NoProfile", "-NoLogo", "-STA", "-ExecutionPolicy", "Bypass", "-WindowStyle", "Hidden", "-File", scriptFile, "-ChoiceFile", resultFile}
	cmdArgs = append(cmdArgs, args...)
	cmd := exec.Command(powershell(), cmdArgs...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("Не удалось показать окно установки: %v (%s)", err, string(out))
	}
	choice, err := os.ReadFile(resultFile)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	} // User clicked Cancel
	return choice, err
}

func validateTarget(path string) (string, error) {
	if path == "" {
		return "", errors.New("Не выбрана папка установки")
	}
	p, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(p) {
		return "", errors.New("Папка установки должна быть абсолютным путём")
	}
	vol := filepath.VolumeName(p)
	if strings.EqualFold(strings.TrimRight(p, `\/`), strings.TrimRight(vol, `\/`)) {
		return "", errors.New("Нельзя устанавливать программу в корень диска")
	}
	if len(strings.TrimPrefix(p, vol)) < 4 {
		return "", errors.New("Выбери отдельную папку для приложения")
	}
	// Don't allow installation directly into Windows, Program Files or common user folders.
	specials := []string{os.Getenv("SystemRoot"), os.Getenv("USERPROFILE"), os.Getenv("LOCALAPPDATA"), os.Getenv("APPDATA")}
	for _, s := range specials {
		if s != "" && strings.EqualFold(filepath.Clean(p), filepath.Clean(s)) {
			return "", errors.New("Выбери отдельную папку внутри этого каталога, а не сам системный каталог")
		}
	}
	if fi, e := os.Lstat(p); e == nil {
		if !fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("Путь установки должен быть обычной папкой")
		}
		entries, err := os.ReadDir(p)
		if err != nil {
			return "", err
		}
		if len(entries) > 0 {
			if err := verifyMarker(p); err != nil {
				if !(len(entries) == 1 && entries[0].Name() == "Data" && entries[0].IsDir()) {
					return "", errors.New("В выбранной папке есть чужие файлы. Выбери пустую папку или папку предыдущей установки Malowar News Agent")
				}
			}
		}
	} else if !os.IsNotExist(e) {
		return "", e
	}
	if err := os.MkdirAll(p, 0700); err != nil {
		return "", fmt.Errorf("Невозможно создать папку %s: %w. Выбери папку без ограничений записи (например, D:\\Apps\\MalowarDownloader)", p, err)
	}
	tmp, err := os.CreateTemp(p, ".write-test-*.tmp")
	if err != nil {
		return "", fmt.Errorf("Нет доступа на запись в %s. Выбери папку вне Program Files или запусти установщик с соответствующими правами: %w", p, err)
	}
	tmp.Close()
	os.Remove(tmp.Name())
	return p, nil
}

type Marker struct {
	ID        string `json:"id"`
	Version   string `json:"version"`
	Directory string `json:"directory"`
	Installed string `json:"installed"`
}

func verifyMarker(dir string) error {
	data, err := os.ReadFile(filepath.Join(dir, markerName))
	if err != nil {
		return err
	}
	var m Marker
	if err := json.Unmarshal(data, &m); err != nil {
		return err
	}
	if m.ID != appID || !strings.EqualFold(filepath.Clean(m.Directory), filepath.Clean(dir)) {
		return errors.New("Папка не принадлежит этому приложению")
	}
	return nil
}

func writeMarker(dir string) error {
	b, err := json.MarshalIndent(Marker{appID, appVersion, dir, time.Now().Format(time.RFC3339)}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, markerName), b, 0600)
}

func writeFile(dst string, contents []byte) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".installer-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err = tmp.Write(contents); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	if err = os.Remove(dst); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.Rename(tmp.Name(), dst)
}

func copyFile(src, dst string) error {
	input, err := os.Open(src)
	if err != nil {
		return err
	}
	defer input.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0700); err != nil {
		return err
	}
	output, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err = io.Copy(output, input); err != nil {
		output.Close()
		return err
	}
	return output.Close()
}

func regAdd(name, typ, value string) error {
	// exec.Command passes each argument without shell interpolation.
	cmd := exec.Command("reg.exe", "ADD", uninstallKey, "/v", name, "/t", typ, "/d", value, "/f")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("Не удалось зарегистрировать программу в списке установленных (%s): %s", name, string(out))
	}
	return nil
}

func registerUninstaller(dir string) error {
	icon := filepath.Join(dir, "Malowar_News_Agent.ico")
	undo := filepath.Join(dir, "Malowar_News_Uninstall.exe")
	for _, pair := range [][3]string{
		{"DisplayName", "REG_SZ", "Malowar News Agent"},
		{"DisplayVersion", "REG_SZ", appVersion},
		{"Publisher", "REG_SZ", "Malowar Gaming"},
		{"InstallLocation", "REG_SZ", dir},
		{"DisplayIcon", "REG_SZ", icon},
		{"UninstallString", "REG_SZ", `"` + undo + `"`},
		{"NoModify", "REG_DWORD", "1"},
		{"NoRepair", "REG_DWORD", "1"},
	} {
		if err := regAdd(pair[0], pair[1], pair[2]); err != nil {
			return err
		}
	}
	return nil
}

func unregister() error {
	cmd := exec.Command("reg.exe", "DELETE", uninstallKey, "/f")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("Не удалось удалить запись из списка установленных программ: %s", string(out))
	}
	return nil
}

func createShortcuts(dir string, desktop bool) error {
	// Installed in the user's Start Menu, never as a system-wide change.
	script := `
param([string]$AppDir,[string]$Desktop)
$ErrorActionPreference='Stop'
$wsh=New-Object -ComObject WScript.Shell
$start=[Environment]::GetFolderPath('Programs')
$lnk=Join-Path $start 'Malowar News Agent.lnk'
$x=$wsh.CreateShortcut($lnk)
$x.TargetPath=Join-Path $AppDir 'Malowar_News_Agent.exe'
$x.WorkingDirectory=$AppDir
$x.IconLocation=Join-Path $AppDir 'Malowar_News_Agent.ico'
$x.Description='Malowar News Agent 1.3.19'
$x.Save()
if($Desktop -eq 'True') {
 $desktopFolder=[Environment]::GetFolderPath('DesktopDirectory')
 $d=$wsh.CreateShortcut((Join-Path $desktopFolder 'Malowar News Agent.lnk'))
 $d.TargetPath=Join-Path $AppDir 'Malowar_News_Agent.exe'
 $d.WorkingDirectory=$AppDir
 $d.IconLocation=Join-Path $AppDir 'Malowar_News_Agent.ico'
 $d.Description='Malowar News Agent 1.3.6'
 $d.Save()
} else {
 $desktopFolder=[Environment]::GetFolderPath('DesktopDirectory')
 $existing=Join-Path $desktopFolder 'Malowar News Agent.lnk'
 if(Test-Path -LiteralPath $existing) {Remove-Item -LiteralPath $existing -Force -ErrorAction SilentlyContinue}
}
`
	_, err := runScript(script, "shortcuts", "-AppDir", dir, "-Desktop", fmt.Sprint(desktop))
	return err
}

func removeShortcuts() error {
	script := `
$paths=@(
 (Join-Path ([Environment]::GetFolderPath('Programs')) 'Malowar News Agent.lnk'),
 (Join-Path ([Environment]::GetFolderPath('DesktopDirectory')) 'Malowar News Agent.lnk')
)
foreach($p in $paths) { if (Test-Path -LiteralPath $p) { Remove-Item -LiteralPath $p -Force -ErrorAction SilentlyContinue } }
`
	_, err := runScript(script, "shortcuts-remove")
	return err
}

func runScript(script, prefix string, args ...string) (string, error) {
	f, err := os.CreateTemp("", prefix+"-*.ps1")
	if err != nil {
		return "", err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(append([]byte{0xef, 0xbb, 0xbf}, []byte(script)...)); err != nil {
		f.Close()
		return "", err
	}
	if err = f.Close(); err != nil {
		return "", err
	}
	params := append([]string{"-NoProfile", "-NoLogo", "-ExecutionPolicy", "Bypass", "-STA", "-WindowStyle", "Hidden", "-File", f.Name()}, args...)
	cmd := exec.Command(powershell(), params...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("PowerShell: %v %s", err, string(output))
	}
	return string(output), nil
}
