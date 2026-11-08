//go:build windows

package servicetray

import (
	"fmt"
	"unsafe"
)

func OpenURL(address string) error {
	result, _, _ := shell.NewProc("ShellExecuteW").Call(0, uintptr(unsafe.Pointer(utf("open"))), uintptr(unsafe.Pointer(utf(address))), 0, 0, 1)
	if result <= 32 {
		return fmt.Errorf("cannot open the browser (%d)", result)
	}
	return nil
}

func RunOpenHelper() error { return fmt.Errorf("the URL helper is macOS only") }

func ShowError(title, message string) { showMessage(0, title, message, false) }
