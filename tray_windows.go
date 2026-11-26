//go:build windows

package main

import (
	"encoding/binary"
	"log"
	"runtime"
	"syscall"
	"unsafe"
)

const (
	trayEvent = 0x8001
	trayStop  = 0x8002
	wmClose   = 0x0010
	wmDestroy = 0x0002
	wmTimer   = 0x0113
)

var (
	trayUser32 = syscall.NewLazyDLL("user32.dll")
	trayShell  = syscall.NewLazyDLL("shell32.dll")
	trayKernel = syscall.NewLazyDLL("kernel32.dll")
)

type trayWindowClass struct {
	style                              uint32
	callback                           uintptr
	classExtra, windowExtra            int32
	instance, icon, cursor, background uintptr
	menu, name                         *uint16
}

type trayPoint struct{ x, y int32 }
type trayMessage struct {
	window         uintptr
	message        uint32
	wParam, lParam uintptr
	time           uint32
	point          trayPoint
	private        uint32
}

// The initial NOTIFYICONDATAW fields suffice for a callback, icon and tooltip.
type trayIconData struct {
	size               uint32
	window             uintptr
	id, flags, message uint32
	icon               uintptr
	tip                [128]uint16
	state, stateMask   uint32
	info               [256]uint16
	version            uint32
	infoTitle          [64]uint16
	infoFlags          uint32
	guid               [16]byte
	balloonIcon        uintptr
}

func appWindowHandle(url string) uintptr {
	title, _ := syscall.UTF16PtrFromString(appWindowTitle(url))
	class, _ := syscall.UTF16PtrFromString("Chrome_WidgetWin_1")
	window, _, _ := trayUser32.NewProc("FindWindowW").Call(uintptr(unsafe.Pointer(class)), uintptr(unsafe.Pointer(title)))
	return window
}

func restoreAppWindow(url string) bool {
	window := appWindowHandle(url)
	if window == 0 {
		return false
	}
	trayUser32.NewProc("ShowWindow").Call(window, 9) // SW_RESTORE
	trayUser32.NewProc("SetForegroundWindow").Call(window)
	return true
}

func loadTrayLogo() uintptr {
	icon, err := webFiles.ReadFile("app/favicon.ico")
	if err == nil && len(icon) >= 22 {
		count := int(binary.LittleEndian.Uint16(icon[4:6]))
		if count <= (len(icon)-6)/16 {
			for i := 0; i < count; i++ {
				entry := icon[6+i*16:][:16]
				if entry[0] != 32 || entry[1] != 32 {
					continue
				}
				size := uint64(binary.LittleEndian.Uint32(entry[8:12]))
				offset := uint64(binary.LittleEndian.Uint32(entry[12:16]))
				if size > 0 && offset+size <= uint64(len(icon)) {
					handle, _, _ := trayUser32.NewProc("CreateIconFromResourceEx").Call(
						uintptr(unsafe.Pointer(&icon[offset])), uintptr(size), 1, 0x00030000, 32, 32, 0)
					runtime.KeepAlive(icon)
					if handle != 0 {
						return handle
					}
				}
			}
		}
	}
	return 0
}

func showTrayMenu(window uintptr) uintptr {
	menu, _, _ := trayUser32.NewProc("CreatePopupMenu").Call()
	if menu == 0 {
		return 0
	}
	defer trayUser32.NewProc("DestroyMenu").Call(menu)
	appendMenu := trayUser32.NewProc("AppendMenuW")
	open, _ := syscall.UTF16PtrFromString("Открыть Malowar News Agent")
	exit, _ := syscall.UTF16PtrFromString("Выход")
	appendMenu.Call(menu, 0, 1, uintptr(unsafe.Pointer(open)))
	appendMenu.Call(menu, 0, 2, uintptr(unsafe.Pointer(exit)))
	var point trayPoint
	trayUser32.NewProc("GetCursorPos").Call(uintptr(unsafe.Pointer(&point)))
	trayUser32.NewProc("SetForegroundWindow").Call(window)
	command, _, _ := trayUser32.NewProc("TrackPopupMenu").Call(menu, 0x102, uintptr(point.x), uintptr(point.y), 0, window, 0)
	trayUser32.NewProc("PostMessageW").Call(window, 0, 0, 0)
	return command
}

func startAppTray(url string, requestExit func()) func() {
	ready := make(chan uintptr, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		var editor uintptr
		var iconData trayIconData
		notify := trayShell.NewProc("Shell_NotifyIconW")
		addIcon := func() bool {
			ok, _, _ := notify.Call(0, uintptr(unsafe.Pointer(&iconData)))
			return ok != 0
		}
		name, _ := syscall.UTF16PtrFromString("MalowarNewsAgentTray")
		taskbar, _ := syscall.UTF16PtrFromString("TaskbarCreated")
		taskbarCreated, _, _ := trayUser32.NewProc("RegisterWindowMessageW").Call(uintptr(unsafe.Pointer(taskbar)))
		callback := syscall.NewCallback(func(window, message, wParam, lParam uintptr) uintptr {
			switch message {
			case trayEvent:
				switch lParam {
				case 0x0202: // WM_LBUTTONUP
					openAppWindow(url)
				case 0x0205: // WM_RBUTTONUP
					switch showTrayMenu(window) {
					case 1:
						openAppWindow(url)
					case 2:
						if current := appWindowHandle(url); current != 0 {
							trayUser32.NewProc("PostMessageW").Call(current, wmClose, 0, 0)
						} else {
							requestExit()
						}
					}
				}
				return 0
			case wmTimer:
				// Track this instance's Edge app window without touching other Edge tabs.
				if editor == 0 {
					editor = appWindowHandle(url)
				}
				if editor != 0 {
					exists, _, _ := trayUser32.NewProc("IsWindow").Call(editor)
					if exists == 0 {
						requestExit()
					} else if minimized, _, _ := trayUser32.NewProc("IsIconic").Call(editor); minimized != 0 {
						visible, _, _ := trayUser32.NewProc("IsWindowVisible").Call(editor)
						if visible != 0 {
							trayUser32.NewProc("ShowWindow").Call(editor, 0) // SW_HIDE
						}
					}
				}
				return 0
			case trayStop:
				trayUser32.NewProc("DestroyWindow").Call(window)
				return 0
			case wmDestroy:
				trayUser32.NewProc("PostQuitMessage").Call(0)
				return 0
			}
			if taskbarCreated != 0 && message == taskbarCreated {
				addIcon()
				return 0
			}
			result, _, _ := trayUser32.NewProc("DefWindowProcW").Call(window, message, wParam, lParam)
			return result
		})
		instance, _, _ := trayKernel.NewProc("GetModuleHandleW").Call(0)
		class := trayWindowClass{callback: callback, instance: instance, name: name}
		atom, _, _ := trayUser32.NewProc("RegisterClassW").Call(uintptr(unsafe.Pointer(&class)))
		if atom == 0 {
			ready <- 0
			return
		}
		window, _, _ := trayUser32.NewProc("CreateWindowExW").Call(0,
			uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(name)), 0,
			0, 0, 0, 0, 0, 0, instance, 0)
		if window == 0 {
			ready <- 0
			return
		}
		icon := loadTrayLogo()
		if icon != 0 {
			defer trayUser32.NewProc("DestroyIcon").Call(icon)
		} else {
			icon, _, _ = trayUser32.NewProc("LoadIconW").Call(0, 32512) // IDI_APPLICATION
		}
		iconData = trayIconData{size: uint32(unsafe.Sizeof(iconData)), window: window, id: 1, flags: 7, message: trayEvent, icon: icon}
		tip, _ := syscall.UTF16FromString("Malowar News Agent")
		copy(iconData.tip[:], tip)
		if !addIcon() {
			trayUser32.NewProc("DestroyWindow").Call(window)
			ready <- 0
			return
		}
		defer notify.Call(2, uintptr(unsafe.Pointer(&iconData)))
		trayUser32.NewProc("SetTimer").Call(window, 1, 350, 0)
		ready <- window
		var msg trayMessage
		for {
			result, _, _ := trayUser32.NewProc("GetMessageW").Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
			if int32(result) <= 0 {
				return
			}
			trayUser32.NewProc("TranslateMessage").Call(uintptr(unsafe.Pointer(&msg)))
			trayUser32.NewProc("DispatchMessageW").Call(uintptr(unsafe.Pointer(&msg)))
		}
	}()
	window := <-ready
	if window == 0 {
		log.Print("Malowar News Agent: Windows tray unavailable")
	}
	return func() {
		if editor := appWindowHandle(url); editor != 0 {
			trayUser32.NewProc("PostMessageW").Call(editor, wmClose, 0, 0)
		}
		if window != 0 {
			trayUser32.NewProc("PostMessageW").Call(window, trayStop, 0, 0)
		}
		<-done
	}
}
