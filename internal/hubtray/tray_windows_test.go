//go:build windows

package hubtray

import (
	"context"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestHubRegistersItsOwnTrayIconAndRemovesItOnExit(t *testing.T) {
	ready := make(chan uintptr, 1)
	closeTray := start(context.Background(), t.TempDir(), func() {}, func(window uintptr) { ready <- window })
	defer closeTray()
	var window uintptr
	select {
	case window = <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("Hub tray window was not created")
	}
	identifier := struct {
		Size   uint32
		Window uintptr
		ID     uint32
		GUID   windows.GUID
	}{Window: window, ID: 1}
	identifier.Size = uint32(unsafe.Sizeof(identifier))
	var rect struct{ Left, Top, Right, Bottom int32 }
	getRect := shell.NewProc("Shell_NotifyIconGetRect")
	result, _, _ := getRect.Call(uintptr(unsafe.Pointer(&identifier)), uintptr(unsafe.Pointer(&rect)))
	if int32(result) < 0 {
		t.Fatalf("Windows did not register the Hub's tray icon: HRESULT=%08x", uint32(result))
	}
	closeTray()
	result, _, _ = getRect.Call(uintptr(unsafe.Pointer(&identifier)), uintptr(unsafe.Pointer(&rect)))
	if int32(result) >= 0 {
		t.Fatal("Hub exit left a registered tray icon")
	}
}
