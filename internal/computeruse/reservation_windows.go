//go:build windows

package computeruse

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	setWindowsHook       = user32.NewProc("SetWindowsHookExW")
	unhookWindowsHook    = user32.NewProc("UnhookWindowsHookEx")
	callNextHook         = user32.NewProc("CallNextHookEx")
	getMessage           = user32.NewProc("GetMessageW")
	peekMessage          = user32.NewProc("PeekMessageW")
	postThreadMessage    = user32.NewProc("PostThreadMessageW")
	kernel32             = windows.NewLazySystemDLL("kernel32.dll")
	getModuleHandle      = kernel32.NewProc("GetModuleHandleW")
	getCurrentThreadID   = kernel32.NewProc("GetCurrentThreadId")
	activeKeyboard       atomic.Pointer[windowReservation]
	physicalEscapeNotify = windows.NewCallback(keyboardHook)
)

type keyboardEvent struct {
	Key, Scan, Flags, Time uint32
	Extra                  uintptr
}

type winMessage struct {
	Window uintptr
	Kind   uint32
	WParam uintptr
	LParam uintptr
	Time   uint32
	X, Y   int32
	Extra  uint32
}

type windowReservation struct {
	threadID    uintptr
	interrupted chan struct{}
	done        chan struct{}
	once        sync.Once
	err         error
}

func physicalEscape(key, flags uint32, message uintptr) bool {
	return key == 0x1b && flags&0x10 == 0 && (message == 0x100 || message == 0x104)
}

func keyboardHook(code, message, pointer uintptr) uintptr {
	if int32(code) >= 0 && pointer != 0 && (message == 0x100 || message == 0x104) {
		var event keyboardEvent
		var copied uintptr
		err := windows.ReadProcessMemory(windows.CurrentProcess(), pointer, (*byte)(unsafe.Pointer(&event)), unsafe.Sizeof(event), &copied)
		if err == nil && copied == unsafe.Sizeof(event) && physicalEscape(event.Key, event.Flags, message) {
			if current := activeKeyboard.Load(); current != nil {
				select {
				case current.interrupted <- struct{}{}:
				default:
				}
			}
		}
	}
	result, _, _ := callNextHook.Call(0, code, message, pointer)
	return result
}

func (r *windowReservation) Interrupted() <-chan struct{} { return r.interrupted }

func (r *windowReservation) Close() error {
	r.once.Do(func() { postThreadMessage.Call(r.threadID, 0x12, 0, 0) }) // WM_QUIT
	<-r.done
	return r.err
}

func (d *nativeDriver) Reserve(ctx context.Context) (Reservation, error) {
	r := &windowReservation{interrupted: make(chan struct{}, 1), done: make(chan struct{})}
	ready := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		defer close(r.done)
		name, _ := windows.UTF16PtrFromString(`Local\MewDex.ComputerUse`)
		mutex, err := windows.CreateMutex(nil, false, name)
		if err != nil && !errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
			ready <- err
			return
		}
		defer func() { r.err = errors.Join(r.err, windows.CloseHandle(mutex)) }()
		state, err := windows.WaitForSingleObject(mutex, 0)
		if err != nil || state != windows.WAIT_OBJECT_0 && state != windows.WAIT_ABANDONED {
			ready <- errors.New("当前桌面已由另一个执行端占用")
			return
		}
		defer func() { r.err = errors.Join(r.err, windows.ReleaseMutex(mutex)) }()
		r.threadID, _, _ = getCurrentThreadID.Call()
		var message winMessage
		peekMessage.Call(uintptr(unsafe.Pointer(&message)), 0, 0, 0, 0)
		module, _, _ := getModuleHandle.Call(0)
		hook, _, hookErr := setWindowsHook.Call(13, physicalEscapeNotify, module, 0)
		if hook == 0 {
			ready <- fmt.Errorf("无法安装本机 Escape 停止处理 %w", hookErr)
			return
		}
		defer func() {
			if ok, _, err := unhookWindowsHook.Call(hook); ok == 0 {
				r.err = errors.Join(r.err, err)
			}
		}()
		activeKeyboard.Store(r)
		defer activeKeyboard.CompareAndSwap(r, nil)
		ready <- nil
		for {
			result, _, messageErr := getMessage.Call(uintptr(unsafe.Pointer(&message)), 0, 0, 0)
			if int32(result) <= 0 {
				if int32(result) < 0 {
					r.err = messageErr
					select {
					case r.interrupted <- struct{}{}:
					default:
					}
				}
				return
			}
		}
	}()
	if err := <-ready; err != nil {
		<-r.done
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		_ = r.Close()
		return nil, err
	}
	return r, nil
}
