//go:build windows

package lifecycle

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

func nativeBootSession() (string, error) {
	// System's process lifetime spans the kernel boot, including suspend and
	// hibernation. Its stored creation identity is immutable when the clock is
	// adjusted; do not use the adjustable system BootTime or a loader BootId.
	// Querying this snapshot does not open another process or require elevation.
	size := uint32(256 << 10)
	for attempt := 0; attempt < 8 && size <= 32<<20; attempt++ {
		buffer := make([]byte, size)
		var returned uint32
		err := windows.NtQuerySystemInformation(windows.SystemProcessInformation, unsafe.Pointer(&buffer[0]), size, &returned)
		if errors.Is(err, windows.STATUS_INFO_LENGTH_MISMATCH) || errors.Is(err, windows.STATUS_BUFFER_TOO_SMALL) {
			size *= 2
			if returned > size {
				size = returned
			}
			continue
		}
		if err != nil {
			return "", fmt.Errorf("%w: kernel process snapshot: %v", ErrBootUnavailable, err)
		}
		if returned == 0 || returned > uint32(len(buffer)) {
			return "", ErrBootUnavailable
		}
		return windowsBootSessionFromProcesses(buffer[:returned])
	}
	return "", ErrBootUnavailable
}

func windowsBootSessionFromProcesses(buffer []byte) (string, error) {
	minimum := int(unsafe.Sizeof(windows.SYSTEM_PROCESS_INFORMATION{}))
	var created int64
	for offset := 0; ; {
		if len(buffer)-offset < minimum {
			return "", ErrBootUnavailable
		}
		process := (*windows.SYSTEM_PROCESS_INFORMATION)(unsafe.Pointer(&buffer[offset]))
		if process.UniqueProcessID == 4 {
			if created != 0 || process.CreateTime <= 0 || process.InheritedFromUniqueProcessID != 0 || process.SessionID != 0 {
				return "", ErrBootUnavailable
			}
			created = process.CreateTime
		}
		if process.NextEntryOffset == 0 {
			break
		}
		next := int(process.NextEntryOffset)
		if next < minimum || next > len(buffer)-offset || next%int(unsafe.Alignof(windows.SYSTEM_PROCESS_INFORMATION{})) != 0 {
			return "", ErrBootUnavailable
		}
		offset += next
	}
	if created == 0 {
		return "", ErrBootUnavailable
	}
	var lifetime [8]byte
	binary.LittleEndian.PutUint64(lifetime[:], uint64(created))
	digest := sha256.Sum256(append([]byte("mewdex:windows-system-kernel-lifetime:v1:"), lifetime[:]...))
	// Use a version-8 UUID representation for the private, derived identity.
	digest[6] = digest[6]&0x0f | 0x80
	digest[8] = digest[8]&0x3f | 0x80
	return NormalizeBootSession(fmt.Sprintf("%x-%x-%x-%x-%x", digest[:4], digest[4:6], digest[6:8], digest[8:10], digest[10:16]))
}
