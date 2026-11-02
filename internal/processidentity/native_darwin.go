//go:build darwin

package processidentity

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"runtime"
	"sync"
	"syscall"
	"unsafe"

	"github.com/ebitengine/purego"
	"golang.org/x/sys/unix"
)

// Darwin's fixed ABI for PROC_PIDT_BSDINFOWITHUNIQID combines the 136-byte
// BSD record with a 56-byte unique-identifier record. No argv/env is read.
// Reference: Apple XNU bsd/sys/proc_info{,_private}.h and proc_internal.h.
func procInfo(call, pid, flavor int, buffer []byte) (int, error) {
	var address unsafe.Pointer
	if len(buffer) != 0 {
		address = unsafe.Pointer(&buffer[0])
	}
	count, _, errno := unix.Syscall6(unix.SYS_PROC_INFO, uintptr(call), uintptr(pid), uintptr(flavor), 0, uintptr(address), uintptr(len(buffer)))
	runtime.KeepAlive(buffer)
	if errno != 0 {
		return 0, errno
	}
	return int(count), nil
}

func Lookup(pid int) (Identity, error) {
	if pid <= 1 {
		return Identity{}, ErrInvalid
	}
	var raw [192]byte
	n, err := procInfo(2, pid, 18, raw[:])
	if err != nil {
		if errors.Is(err, syscall.EPERM) {
			return lookupReadable(pid)
		}
		return Identity{}, fmt.Errorf("read process %d identity: %w", pid, err)
	}
	if n != len(raw) {
		return Identity{}, ErrUnsupported
	}
	word := func(offset int) uint32 { return binary.LittleEndian.Uint32(raw[offset : offset+4]) }
	value := Identity{PID: int(word(12)), ParentPID: int(word(16)), UID: word(20), Group: int(word(100)), State: word(4),
		UniqueID: binary.LittleEndian.Uint64(raw[152:160]), ParentUniqueID: binary.LittleEndian.Uint64(raw[160:168]), PIDVersion: word(168)}
	if value.PID != pid || !value.valid() {
		return Identity{}, ErrUnsupported
	}
	return value, nil
}

// The composite BSD flavor requires equal effective users. A legitimate child
// may exec a system setuid utility (for example /bin/ps). XNU explicitly permits
// the short BSD and unique-ID flavors without that credential-equality check.
// Bracket the short record with matching immutable/exec identities; this does
// not grant signal permission or fall back to a PID-only control operation.
func lookupReadable(pid int) (Identity, error) {
	for attempt := 0; attempt < 3; attempt++ {
		var before, after [56]byte
		var short [64]byte
		for _, item := range []struct {
			flavor int
			raw    []byte
		}{{17, before[:]}, {13, short[:]}, {17, after[:]}} {
			n, err := procInfo(2, pid, item.flavor, item.raw)
			if err != nil {
				return Identity{}, fmt.Errorf("read process %d public identity: %w", pid, err)
			}
			if n != len(item.raw) {
				return Identity{}, ErrUnsupported
			}
		}
		if string(before[16:40]) != string(after[16:40]) {
			continue
		}
		word := func(offset int) uint32 { return binary.LittleEndian.Uint32(short[offset : offset+4]) }
		value := Identity{PID: int(word(0)), ParentPID: int(word(4)), Group: int(word(8)), State: word(12), UID: word(36), UniqueID: binary.LittleEndian.Uint64(after[16:24]), ParentUniqueID: binary.LittleEndian.Uint64(after[24:32]), PIDVersion: binary.LittleEndian.Uint32(after[32:36])}
		if value.PID != pid || !value.valid() {
			return Identity{}, ErrUnsupported
		}
		return value, nil
	}
	return Identity{}, ErrChanged
}

func ListPIDs() ([]int, error) {
	needed, err := procInfo(1, 1, 0, nil)
	if err != nil {
		return nil, err
	}
	if needed <= 0 || needed > maxProcesses*4-2048 {
		return nil, ErrLimit
	}
	buffer := make([]byte, needed+2048)
	n, err := procInfo(1, 1, 0, buffer)
	if err != nil {
		return nil, err
	}
	if n < 0 || n >= len(buffer) || n%4 != 0 {
		return nil, ErrLimit
	}
	values := make([]int, 0, n/4)
	for offset := 0; offset < n; offset += 4 {
		if pid := int(int32(binary.LittleEndian.Uint32(buffer[offset : offset+4]))); pid > 1 {
			values = append(values, pid)
		}
	}
	return values, nil
}

var signalAPI struct {
	once sync.Once
	call func(unsafe.Pointer, int32) int32
	err  error
}

func loadSignal() {
	// libproc lives in the dyld cache; a filesystem existence check is invalid.
	library, err := purego.Dlopen("/usr/lib/libproc.dylib", purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		signalAPI.err = errors.Join(ErrUnsupported, err)
		return
	}
	symbol, err := purego.Dlsym(library, "proc_signal_with_audittoken")
	if err != nil {
		_ = purego.Dlclose(library)
		signalAPI.err = errors.Join(ErrUnsupported, err)
		return
	}
	purego.RegisterFunc(&signalAPI.call, symbol)
}

func Available() error {
	if _, err := Lookup(os.Getpid()); err != nil {
		return err
	}
	signalAPI.once.Do(loadSignal)
	return signalAPI.err
}

// Signal revalidates the immutable lifetime and exec generation, then asks the
// kernel to validate PID+pidversion atomically with delivery. A missing API
// never falls back to kill(pid). Credentials come from the real caller/target;
// the audit-token argument is a target identity, not a delegated instigator.
func Signal(expected Identity, signal syscall.Signal) error {
	if !expected.valid() || (signal != syscall.SIGTERM && signal != syscall.SIGKILL) {
		return ErrInvalid
	}
	current, err := Lookup(expected.PID)
	if err != nil {
		return err
	}
	if !expected.SameLifetime(current) || expected.PIDVersion != current.PIDVersion {
		return ErrChanged
	}
	if current.UID != uint32(os.Geteuid()) {
		return errors.New("process identity is outside the current user's scope")
	}
	signalAPI.once.Do(loadSignal)
	if signalAPI.err != nil {
		return signalAPI.err
	}
	var token [8]uint32
	token[5], token[7] = uint32(current.PID), current.PIDVersion
	result := signalAPI.call(unsafe.Pointer(&token), int32(signal))
	runtime.KeepAlive(token)
	if result != 0 {
		return syscall.Errno(result)
	}
	return nil
}
