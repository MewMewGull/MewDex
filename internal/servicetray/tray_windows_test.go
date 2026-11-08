//go:build windows

package servicetray

import (
	"context"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestServiceRegistersItsTrayAndRemovesItOnExit(t *testing.T) {
	ready := make(chan uintptr, 1)
	closeTray, err := start(context.Background(), Options{Title: "MewDex tray test", Menu: func() []Item { return []Item{{ID: "open", Label: "Open"}} }, Action: func(context.Context, string) (string, error) { return "", nil }}, func(window uintptr) { ready <- window })
	if err != nil {
		t.Fatal(err)
	}
	defer closeTray()
	var window uintptr
	select {
	case window = <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("tray window was not created")
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
		t.Fatalf("tray icon registration failed HRESULT=%08x", uint32(result))
	}
	closeTray()
	result, _, _ = getRect.Call(uintptr(unsafe.Pointer(&identifier)), uintptr(unsafe.Pointer(&rect)))
	if int32(result) >= 0 {
		t.Fatal("service exit left its tray icon registered")
	}
}
