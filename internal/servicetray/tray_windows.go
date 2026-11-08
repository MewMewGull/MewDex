//go:build windows

package servicetray

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var traySequence atomic.Uint64

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

func utf(value string) *uint16 {
	result, _ := windows.UTF16PtrFromString(strings.ReplaceAll(value, "\x00", "�"))
	return result
}

func Start(parent context.Context, options Options) (func(), error) {
	return start(parent, options, nil)
}

func start(parent context.Context, options Options, ready func(uintptr)) (func(), error) {
	if options.Title == "" || options.Menu == nil || options.Action == nil {
		return nil, errors.New("service tray requires its title, menu and action handler")
	}
	ctx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	initialized := make(chan error, 1)
	go func() {
		defer close(done)
		run(ctx, options, ready, initialized)
	}()
	var once sync.Once
	closeTray := func() { once.Do(func() { cancel(); <-done }) }
	select {
	case err := <-initialized:
		if err != nil {
			closeTray()
			return nil, err
		}
		return closeTray, nil
	case <-parent.Done():
		closeTray()
		return nil, parent.Err()
	}
}

func run(ctx context.Context, options Options, ready func(uintptr), initialized chan<- error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	var window uintptr
	var busy atomic.Bool
	var icon iconData
	registerIcon := func() {
		added, _, _ := notifyIcon.Call(0, uintptr(unsafe.Pointer(&icon)))
		if added != 0 {
			user.NewProc("KillTimer").Call(window, 1)
		} else {
			user.NewProc("SetTimer").Call(window, 1, 1000, 0)
		}
	}
	recreated, _, _ := registerMessage.Call(uintptr(unsafe.Pointer(utf("TaskbarCreated"))))
	execute := func(id string) {
		if !busy.CompareAndSwap(false, true) {
			return
		}
		go func() {
			defer busy.Store(false)
			operation, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			text, err := options.Action(operation, id)
			if err != nil {
				text = err.Error()
			}
			if text != "" && ctx.Err() == nil {
				showMessage(window, options.Title, text, false)
			}
		}()
	}
	callback := syscall.NewCallback(func(hwnd uintptr, msg uint32, wparam, lparam uintptr) uintptr {
		switch {
		case msg == uint32(recreated) && recreated != 0:
			registerIcon()
		case msg == 0x0113:
			registerIcon()
		case msg == 0x8001 && (lparam == 0x0203 || lparam == 0x0202):
			execute("open")
		case msg == 0x8001 && (lparam == 0x0205 || lparam == 0x007B):
			menu, _, _ := createMenu.Call()
			if menu == 0 {
				break
			}
			defer destroyMenu.Call(menu)
			items := options.Menu()
			for index, item := range items {
				flags := uintptr(0)
				if item.Separator {
					flags = 0x800
				} else if busy.Load() {
					flags = 1
				}
				if item.Checked {
					flags |= 8
				}
				appendMenu.Call(menu, flags, uintptr(index+1), uintptr(unsafe.Pointer(utf(item.Label))))
			}
			var cursor point
			getCursor.Call(uintptr(unsafe.Pointer(&cursor)))
			foreground.Call(hwnd)
			selected, _, _ := trackMenu.Call(menu, 0x100|0x2, uintptr(cursor.X), uintptr(cursor.Y), 0, hwnd, 0)
			postMessage.Call(hwnd, 0, 0, 0)
			if selected > 0 && int(selected) <= len(items) {
				item := items[selected-1]
				if !item.Separator && (item.Question == "" || showMessage(hwnd, options.Title, item.Question, true)) {
					execute(item.ID)
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
	class := windowClass{Proc: callback, Name: utf(fmt.Sprintf("MewDexServiceTray%d_%d", os.Getpid(), traySequence.Add(1)))}
	class.Size = uint32(unsafe.Sizeof(class))
	registered, _, classErr := registerClass.Call(uintptr(unsafe.Pointer(&class)))
	if registered == 0 {
		initialized <- fmt.Errorf("cannot register service tray %w", classErr)
		return
	}
	defer user.NewProc("UnregisterClassW").Call(uintptr(unsafe.Pointer(class.Name)), 0)
	window, _, _ = createWindow.Call(0, uintptr(unsafe.Pointer(class.Name)), uintptr(unsafe.Pointer(utf(options.Title))), 0, 0, 0, 0, 0, 0, 0, 0, 0)
	if window == 0 {
		initialized <- errors.New("cannot create service tray window")
		return
	}
	icon = iconData{Window: window, ID: 1, Flags: 1 | 2 | 4, Callback: 0x8001}
	icon.Size = uint32(unsafe.Sizeof(icon))
	var ownedIcon bool
	icon.Icon, ownedIcon = serviceIcon(options.Role)
	if ownedIcon {
		defer user.NewProc("DestroyIcon").Call(icon.Icon)
	}
	copy(icon.Tip[:], windows.StringToUTF16(options.Title))
	// Explorer may start after an interactive logon task has already launched.
	registerIcon()
	defer user.NewProc("KillTimer").Call(window, 1)
	defer notifyIcon.Call(2, uintptr(unsafe.Pointer(&icon)))
	closeOnCancel := context.AfterFunc(ctx, func() { postMessage.Call(window, 0x0010, 0, 0) })
	defer closeOnCancel()
	if ready != nil {
		ready(window)
	}
	initialized <- nil
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

func serviceIcon(role Role) (uintptr, bool) {
	iconResource := iconBytes(role, "ico")
	if len(iconResource) >= 6 {
		count := int(binary.LittleEndian.Uint16(iconResource[4:6]))
		for index := 0; index < count && 6+(index+1)*16 <= len(iconResource); index++ {
			entry := iconResource[6+index*16 : 6+(index+1)*16]
			if entry[0] != 32 || entry[1] != 32 {
				continue
			}
			size, offset := int(binary.LittleEndian.Uint32(entry[8:12])), int(binary.LittleEndian.Uint32(entry[12:16]))
			if size > 0 && offset >= 0 && offset <= len(iconResource)-size {
				value, _, _ := user.NewProc("CreateIconFromResourceEx").Call(uintptr(unsafe.Pointer(&iconResource[offset])), uintptr(size), 1, 0x30000, 32, 32, 0)
				if value != 0 {
					return value, true
				}
			}
		}
	}
	value, _, _ := loadIcon.Call(0, 32512)
	return value, false
}

func showMessage(window uintptr, title, text string, question bool) bool {
	flags := uintptr(0x40)
	if question {
		flags = 0x131 // Confirm or cancel, with cancel selected by default.
	}
	result, _, _ := messageBox.Call(window, uintptr(unsafe.Pointer(utf(text))), uintptr(unsafe.Pointer(utf(title))), flags)
	return result == 1
}

func RunHelper() error { return errors.New("Windows service tray runs in its owner process") }
