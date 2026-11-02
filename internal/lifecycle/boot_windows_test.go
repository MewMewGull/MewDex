//go:build windows

package lifecycle

import (
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func kernelProcessSnapshot(values ...windows.SYSTEM_PROCESS_INFORMATION) []byte {
	size := int(unsafe.Sizeof(windows.SYSTEM_PROCESS_INFORMATION{}))
	buffer := make([]byte, size*len(values))
	for i, value := range values {
		if i+1 < len(values) {
			value.NextEntryOffset = uint32(size)
		}
		*(*windows.SYSTEM_PROCESS_INFORMATION)(unsafe.Pointer(&buffer[i*size])) = value
	}
	return buffer
}

func TestWindowsBootIdentityUsesOnlyTheImmutableKernelLifetime(t *testing.T) {
	kernel := windows.SYSTEM_PROCESS_INFORMATION{UniqueProcessID: 4, CreateTime: 134000000000000000}
	first, err := windowsBootSessionFromProcesses(kernelProcessSnapshot(kernel))
	if err != nil {
		t.Fatal(err)
	}
	// Login, workload, process names and CPU time do not define a new boot.
	kernel.CycleTime, kernel.KernelTime, kernel.NumberOfThreads = 99, 777, 40
	second, err := windowsBootSessionFromProcesses(kernelProcessSnapshot(windows.SYSTEM_PROCESS_INFORMATION{UniqueProcessID: 0}, kernel, windows.SYSTEM_PROCESS_INFORMATION{UniqueProcessID: 900, CreateTime: kernel.CreateTime + 1, SessionID: 3}))
	if err != nil || second != first {
		t.Fatal("unrelated snapshot changes authorized a new boot", first, second, err)
	}
	kernel.CreateTime++
	third, err := windowsBootSessionFromProcesses(kernelProcessSnapshot(kernel))
	if err != nil || third == first {
		t.Fatal("new kernel lifetime was not distinguished", third, err)
	}
	for _, invalid := range [][]byte{nil, {0}, kernelProcessSnapshot(windows.SYSTEM_PROCESS_INFORMATION{UniqueProcessID: 0}), kernelProcessSnapshot(kernel, kernel), kernelProcessSnapshot(windows.SYSTEM_PROCESS_INFORMATION{UniqueProcessID: 4}), kernelProcessSnapshot(windows.SYSTEM_PROCESS_INFORMATION{UniqueProcessID: 4, CreateTime: 1, SessionID: 1})} {
		if value, err := windowsBootSessionFromProcesses(invalid); err == nil || value != "" {
			t.Fatal("invalid snapshot invented boot proof", value, err)
		}
	}
	malformed := kernelProcessSnapshot(kernel, kernel)
	(*windows.SYSTEM_PROCESS_INFORMATION)(unsafe.Pointer(&malformed[0])).NextEntryOffset = 1
	if _, err := windowsBootSessionFromProcesses(malformed); err == nil {
		t.Fatal("misaligned process snapshot was accepted")
	}
}

func TestWindowsBootSessionUsesCurrentKernelLifetime(t *testing.T) {
	first, err := BootSession()
	if err != nil {
		t.Fatal(err)
	}
	second, err := BootSession()
	if err != nil || first != second {
		t.Fatal("current OS lifetime was unstable", first, second, err)
	}
	t.Logf("currentKernelLifetime=%s; no system restart or clock change performed", first)
}
