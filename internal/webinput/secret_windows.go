//go:build windows

package webinput

import (
	"errors"
	"unsafe"

	"golang.org/x/sys/windows"
)

func protect(raw []byte) ([]byte, error)   { return crypt(raw, true) }
func unprotect(raw []byte) ([]byte, error) { return crypt(raw, false) }

func crypt(raw []byte, encrypt bool) ([]byte, error) {
	if len(raw) == 0 {
		return nil, errors.New("empty connection")
	}
	input := windows.DataBlob{Size: uint32(len(raw)), Data: &raw[0]}
	var output windows.DataBlob
	var err error
	if encrypt {
		err = windows.CryptProtectData(&input, nil, nil, 0, nil, 1, &output)
	} else {
		err = windows.CryptUnprotectData(&input, nil, nil, 0, nil, 1, &output)
	}
	if err != nil {
		return nil, err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(output.Data)))
	return append([]byte(nil), unsafe.Slice(output.Data, output.Size)...), nil
}
