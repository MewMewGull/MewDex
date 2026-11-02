//go:build windows

// The Hub owns this icon and message loop. There is no tray helper process.
package hubtray

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"mewdex/internal/hubproxy"
	"mewdex/internal/localnode"
)

var (
	user             = windows.NewLazySystemDLL("user32.dll")
	shell            = windows.NewLazySystemDLL("shell32.dll")
	registerClass    = user.NewProc("RegisterClassExW")
	createWindow     = user.NewProc("CreateWindowExW")
	defWindow        = user.NewProc("DefWindowProcW")
	destroyWindow    = user.NewProc("DestroyWindow")
	getMessage       = user.NewProc("GetMessageW")
	translateMessage = user.NewProc("TranslateMessage")
	dispatchMessage  = user.NewProc("DispatchMessageW")
	postMessage      = user.NewProc("PostMessageW")
	postQuit         = user.NewProc("PostQuitMessage")
	loadIcon         = user.NewProc("LoadIconW")
	createMenu       = user.NewProc("CreatePopupMenu")
	appendMenu       = user.NewProc("AppendMenuW")
	trackMenu        = user.NewProc("TrackPopupMenu")
	destroyMenu      = user.NewProc("DestroyMenu")
	getCursor        = user.NewProc("GetCursorPos")
	foreground       = user.NewProc("SetForegroundWindow")
	messageBox       = user.NewProc("MessageBoxW")
	registerMessage  = user.NewProc("RegisterWindowMessageW")
	notifyIcon       = shell.NewProc("Shell_NotifyIconW")
)

type point struct{ X, Y int32 }
type message struct {
	Window         uintptr
	Message        uint32
	Wparam, Lparam uintptr
	Time           uint32
	Point          point
	Private        uint32
}
type windowClass struct {
	Size, Style                        uint32
	Proc                               uintptr
	ClassExtra, WindowExtra            int32
	Instance, Icon, Cursor, Background uintptr
	Menu, Name                         *uint16
	SmallIcon                          uintptr
}
type iconData struct {
	Size                uint32
	Window              uintptr
	ID, Flags, Callback uint32
	Icon                uintptr
	Tip                 [128]uint16
	State, StateMask    uint32
	Info                [256]uint16
	Timeout             uint32
	Title               [64]uint16
	InfoFlags           uint32
	GUID                windows.GUID
	BalloonIcon         uintptr
}

func utf(s string) *uint16 { p, _ := windows.UTF16PtrFromString(s); return p }

func Start(parent context.Context, root string, stopHub context.CancelFunc) func() {
	ctx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	go func() { defer close(done); run(ctx, root, stopHub) }()
	return func() { cancel(); <-done }
}

func run(ctx context.Context, root string, stopHub context.CancelFunc) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	var window uintptr
	var busy atomic.Bool
	program, _ := os.Executable()
	manager, err := localnode.New(localnode.Options{Root: root, Program: program})
	if err != nil {
		return
	}
	var icon iconData
	recreated, _, _ := registerMessage.Call(uintptr(unsafe.Pointer(utf("TaskbarCreated"))))
	callback := syscall.NewCallback(func(hwnd uintptr, msg uint32, wparam, lparam uintptr) uintptr {
		switch {
		case msg == uint32(recreated) && recreated != 0:
			notifyIcon.Call(0, uintptr(unsafe.Pointer(&icon)))
		case msg == 0x8001 && (lparam == 0x0203 || lparam == 0x0202):
			openWorkbench(root)
		case msg == 0x8001 && (lparam == 0x0205 || lparam == 0x007B):
			menu, _, _ := createMenu.Call()
			defer destroyMenu.Call(menu)
			add := func(id uintptr, text string, flags uintptr) {
				appendMenu.Call(menu, flags, id, uintptr(unsafe.Pointer(utf(text))))
			}
			add(1, "打开 MewDex", 0)
			add(2, "查看 Hub 状态", 0)
			add(0, "", 0x800)
			settings, _, _ := hubproxy.Read(root)
			proxyLabel := "启用公网反向代理"
			if settings.Enabled {
				proxyLabel = "关闭公网反向代理"
			}
			flags := uintptr(0)
			if busy.Load() {
				flags = 3
			}
			add(3, proxyLabel, flags)
			add(4, "切换开机自启动…", flags)
			add(0, "", 0x800)
			add(5, "退出 MewDex Hub（停止后台服务）", 0)
			var cursor point
			getCursor.Call(uintptr(unsafe.Pointer(&cursor)))
			foreground.Call(hwnd)
			selected, _, _ := trackMenu.Call(menu, 0x100|0x2, uintptr(cursor.X), uintptr(cursor.Y), 0, hwnd, 0)
			postMessage.Call(hwnd, 0, 0, 0)
			switch selected {
			case 1:
				openWorkbench(root)
			case 2, 3, 4:
				if !busy.CompareAndSwap(false, true) {
					break
				}
				go func() {
					defer busy.Store(false)
					operation, cancel := context.WithTimeout(ctx, 30*time.Second)
					defer cancel()
					result, err := manager.Handle(operation, localnode.Request{Operation: "status"})
					if err == nil {
						switch selected {
						case 2:
							status, statusErr := manager.Handle(operation, localnode.Request{Operation: "proxy.status"})
							err = statusErr
							if err == nil {
								text := "Hub：" + result.Snapshot.Name + "\n后台服务：" + result.Snapshot.State
								if status.HubProxy != nil {
									text += "\n公网代理：" + status.HubProxy.State + "\n" + status.HubProxy.Message
								}
								showMessage(window, text, false)
							}
						case 3:
							settings.Enabled = !settings.Enabled
							_, err = manager.Handle(operation, localnode.Request{Operation: "proxy.apply", ExpectedDeviceID: result.Snapshot.DeviceID, HubProxy: (*localnode.HubProxySettings)(&settings)})
						case 4:
							status, statusErr := manager.Handle(operation, localnode.Request{Operation: "startup.status"})
							err = statusErr
							if err == nil && status.Startup != nil {
								enable := !status.Startup.RequestedEnabled
								question := "启用开机自启动？登录 Windows 后将静默启动 MewDex Hub 到托盘。"
								if !enable {
									question = "关闭开机自启动？当前 Hub 将继续运行。"
								}
								if showMessage(window, question, true) {
									_, err = manager.Handle(operation, localnode.Request{Operation: "startup.apply", ExpectedDeviceID: result.Snapshot.DeviceID, Startup: &localnode.StartupRequest{Enabled: enable}})
								}
							}
						}
					}
					if err != nil {
						showMessage(window, err.Error(), false)
					}
				}()
			case 5:
				if showMessage(hwnd, "退出 Hub 将断开公网入口并停止本机服务，继续退出？", true) {
					stopHub()
				}
			}
		case msg == 0x0010:
			destroyWindow.Call(hwnd)
		case msg == 0x0002:
			postQuit.Call(0)
		default:
			value, _, _ := defWindow.Call(hwnd, uintptr(msg), wparam, lparam)
			return value
		}
		return 0
	})
	class := windowClass{Proc: callback, Name: utf("MewDexHubTray")}
	class.Size = uint32(unsafe.Sizeof(class))
	registerClass.Call(uintptr(unsafe.Pointer(&class)))
	window, _, _ = createWindow.Call(0, uintptr(unsafe.Pointer(class.Name)), uintptr(unsafe.Pointer(utf("MewDex Hub"))), 0, 0, 0, 0, 0, 0, 0, 0, 0)
	if window == 0 {
		return
	}
	icon = iconData{Window: window, ID: 1, Flags: 1 | 2 | 4, Callback: 0x8001}
	icon.Size = uint32(unsafe.Sizeof(icon))
	icon.Icon, _, _ = loadIcon.Call(0, 32512)
	copy(icon.Tip[:], windows.StringToUTF16("MewDex Hub · 后台服务与公网代理"))
	notifyIcon.Call(0, uintptr(unsafe.Pointer(&icon)))
	defer notifyIcon.Call(2, uintptr(unsafe.Pointer(&icon)))
	closeOnCancel := context.AfterFunc(ctx, func() { postMessage.Call(window, 0x0010, 0, 0) })
	defer closeOnCancel()
	var msg message
	for {
		result, _, _ := getMessage.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if result == 0 || int32(result) == -1 {
			break
		}
		translateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		dispatchMessage.Call(uintptr(unsafe.Pointer(&msg)))
	}
	runtime.KeepAlive(callback)
}

func showMessage(window uintptr, text string, question bool) bool {
	flags := uintptr(0x40)
	if question {
		flags = 0x24
	}
	result, _, _ := messageBox.Call(window, uintptr(unsafe.Pointer(utf(text))), uintptr(unsafe.Pointer(utf("MewDex Hub"))), flags)
	return result == 6
}

func openWorkbench(root string) {
	raw, err := os.ReadFile(filepath.Join(root, "workbench.json"))
	var launcher struct {
		Path string `json:"path"`
	}
	if err != nil || len(raw) > 32<<10 || json.Unmarshal(raw, &launcher) != nil || !filepath.IsAbs(launcher.Path) {
		return
	}
	command := exec.Command(launcher.Path)
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if command.Start() == nil {
		go command.Wait()
	}
}
