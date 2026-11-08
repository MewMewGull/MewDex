//go:build windows

package computeruse

import (
	"testing"
	"unsafe"

	"mewdex/internal/protocol"
)

func TestNativeInputLayoutUnicodeAndPhysicalEscapeFiltering(t *testing.T) {
	if unsafe.Sizeof(winInput{}) != 40 || unsafe.Sizeof(bitmapInfo{}) != 40 || unsafe.Sizeof(winMessage{}) != 48 || unsafe.Sizeof(keyboardInput{}) != 24 || unsafe.Sizeof(mouseInput{}) != 32 {
		t.Fatal("Windows ABI layout changed")
	}
	inputs, err := actionInputs(nativeRequest{Action: protocol.ComputerAction{Kind: "type", Text: "中😀\n"}})
	if err != nil || len(inputs) != 8 {
		t.Fatal(len(inputs), err)
	}
	first := (*keyboardInput)(unsafe.Pointer(&inputs[0].Data[0]))
	last := (*keyboardInput)(unsafe.Pointer(&inputs[len(inputs)-1].Data[0]))
	if first.Scan != '中' || first.Flags != 4 || last.Key != 0x0d || last.Flags != 2 {
		t.Fatal(first, last)
	}
	if _, err = actionInputs(nativeRequest{Action: protocol.ComputerAction{Kind: "key", Keys: []string{"WIN", "R"}}}); err == nil {
		t.Fatal("desktop-global shortcut was accepted")
	}
	if !physicalEscape(0x1b, 0, 0x100) || physicalEscape(0x1b, 0x10, 0x100) || physicalEscape(0x1b, 0, 0x101) || physicalEscape(0x41, 0, 0x100) {
		t.Fatal("injected input can impersonate physical Escape")
	}
}
