//go:build windows

package computeruse

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"runtime"
	"strconv"
	"strings"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
	"mewdex/internal/protocol"
)

var (
	user32                 = windows.NewLazySystemDLL("user32.dll")
	gdi32                  = windows.NewLazySystemDLL("gdi32.dll")
	enumWindows            = user32.NewProc("EnumWindows")
	getWindowText          = user32.NewProc("GetWindowTextW")
	isWindowVisible        = user32.NewProc("IsWindowVisible")
	isWindow               = user32.NewProc("IsWindow")
	isIconic               = user32.NewProc("IsIconic")
	getWindowThreadProcess = user32.NewProc("GetWindowThreadProcessId")
	getWindowRect          = user32.NewProc("GetWindowRect")
	windowFromPoint        = user32.NewProc("WindowFromPoint")
	getAncestor            = user32.NewProc("GetAncestor")
	getForegroundWindow    = user32.NewProc("GetForegroundWindow")
	setForegroundWindow    = user32.NewProc("SetForegroundWindow")
	getSystemMetrics       = user32.NewProc("GetSystemMetrics")
	getAsyncKeyState       = user32.NewProc("GetAsyncKeyState")
	getWindowStation       = user32.NewProc("GetProcessWindowStation")
	getUserObjectInfo      = user32.NewProc("GetUserObjectInformationW")
	setThreadDPI           = user32.NewProc("SetThreadDpiAwarenessContext")
	sendInput              = user32.NewProc("SendInput")
	getDC                  = user32.NewProc("GetDC")
	releaseDC              = user32.NewProc("ReleaseDC")
	printWindow            = user32.NewProc("PrintWindow")
	createCompatibleDC     = gdi32.NewProc("CreateCompatibleDC")
	createCompatibleBitmap = gdi32.NewProc("CreateCompatibleBitmap")
	selectObject           = gdi32.NewProc("SelectObject")
	deleteObject           = gdi32.NewProc("DeleteObject")
	deleteDC               = gdi32.NewProc("DeleteDC")
	getDIBits              = gdi32.NewProc("GetDIBits")
)

type winRect struct{ Left, Top, Right, Bottom int32 }

func prepareNative() (func(), error) {
	runtime.LockOSThread()
	previous, _, err := setThreadDPI.Call(^uintptr(3)) // DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2.
	if previous == 0 {
		runtime.UnlockOSThread()
		return nil, fmt.Errorf("无法建立物理像素坐标上下文 %w", err)
	}
	return func() { setThreadDPI.Call(previous); runtime.UnlockOSThread() }, nil
}

func (d *nativeDriver) Available() (bool, string) {
	if runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64" {
		return false, "Computer Use 当前需要 64 位 Windows"
	}
	station, _, _ := getWindowStation.Call()
	var flags struct {
		Inherit, Reserved int32
		Flags             uint32
	}
	var needed uint32
	ok, _, _ := getUserObjectInfo.Call(station, 1, uintptr(unsafe.Pointer(&flags)), unsafe.Sizeof(flags), uintptr(unsafe.Pointer(&needed)))
	if station == 0 || ok == 0 || flags.Flags&1 == 0 {
		return false, "执行端需要运行在已登录用户的可交互桌面中"
	}
	return true, ""
}

func windowHandle(value protocol.ComputerWindow) (uintptr, Bounds, error) {
	handle, err := strconv.ParseUint(value.ID, 16, 64)
	if err != nil || handle == 0 {
		return 0, Bounds{}, errors.New("invalid window identity")
	}
	hwnd := uintptr(handle)
	valid, _, _ := isWindow.Call(hwnd)
	visible, _, _ := isWindowVisible.Call(hwnd)
	minimized, _, _ := isIconic.Call(hwnd)
	var pid uint32
	getWindowThreadProcess.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	var rect winRect
	ok, _, _ := getWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&rect)))
	bounds := Bounds{Left: int(rect.Left), Top: int(rect.Top), Width: int(rect.Right - rect.Left), Height: int(rect.Bottom - rect.Top)}
	if valid == 0 || visible == 0 || minimized != 0 || value.ProcessID == 0 || value.ProcessID != pid || ok == 0 || bounds.Width <= 0 || bounds.Height <= 0 || bounds.Width > 8192 || bounds.Height > 8192 || bounds.Width*bounds.Height > 16<<20 {
		return 0, Bounds{}, errors.New("授权窗口已关闭、最小化、变化或超出捕获范围")
	}
	return hwnd, bounds, nil
}

func nativeWindows() ([]protocol.ComputerWindow, error) {
	result := []protocol.ComputerWindow{}
	callback := windows.NewCallback(func(hwnd, _ uintptr) uintptr {
		visible, _, _ := isWindowVisible.Call(hwnd)
		if visible == 0 {
			return 1
		}
		buffer := make([]uint16, 513)
		length, _, _ := getWindowText.Call(hwnd, uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)))
		if length == 0 {
			return 1
		}
		var pid uint32
		getWindowThreadProcess.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
		window := protocol.ComputerWindow{ID: strconv.FormatUint(uint64(hwnd), 16), ProcessID: pid, Title: windows.UTF16ToString(buffer)}
		if _, _, err := windowHandle(window); err == nil {
			result = append(result, window)
		}
		if len(result) >= 256 {
			return 0
		}
		return 1
	})
	ok, _, err := enumWindows.Call(callback, 0)
	if ok == 0 && len(result) == 0 {
		return nil, fmt.Errorf("读取交互桌面的窗口失败 %w", err)
	}
	return result, nil
}

type bitmapInfo struct {
	Size          uint32
	Width, Height int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPels, YPels  int32
	ColorsUsed    uint32
	Important     uint32
}

func nativeObserve(window protocol.ComputerWindow) (protocol.ComputerObservation, Bounds, error) {
	hwnd, bounds, err := windowHandle(window)
	if err != nil {
		return protocol.ComputerObservation{}, bounds, err
	}
	dc, _, _ := getDC.Call(0)
	if dc == 0 {
		return protocol.ComputerObservation{}, bounds, errors.New("交互桌面的图像设备不可用")
	}
	defer releaseDC.Call(0, dc)
	memory, _, _ := createCompatibleDC.Call(dc)
	if memory == 0 {
		return protocol.ComputerObservation{}, bounds, errors.New("无法创建窗口图像上下文")
	}
	defer deleteDC.Call(memory)
	bitmap, _, _ := createCompatibleBitmap.Call(dc, uintptr(bounds.Width), uintptr(bounds.Height))
	if bitmap == 0 {
		return protocol.ComputerObservation{}, bounds, errors.New("无法分配窗口图像")
	}
	defer deleteObject.Call(bitmap)
	previous, _, _ := selectObject.Call(memory, bitmap)
	ok, _, _ := printWindow.Call(hwnd, memory, 2) // PW_RENDERFULLCONTENT on supported Windows.
	selectObject.Call(memory, previous)
	if ok == 0 {
		return protocol.ComputerObservation{}, bounds, errors.New("该窗口未能提供图像，请选择其他窗口")
	}
	info := bitmapInfo{Size: uint32(unsafe.Sizeof(bitmapInfo{})), Width: int32(bounds.Width), Height: -int32(bounds.Height), Planes: 1, BitCount: 32}
	pixels := make([]byte, bounds.Width*bounds.Height*4)
	lines, _, _ := getDIBits.Call(dc, bitmap, 0, uintptr(bounds.Height), uintptr(unsafe.Pointer(&pixels[0])), uintptr(unsafe.Pointer(&info)), 0)
	if int(lines) != bounds.Height {
		return protocol.ComputerObservation{}, bounds, errors.New("窗口图像读取不完整")
	}
	width, height := bounds.Width, bounds.Height
	if width > 1280 {
		height, width = max(1, height*1280/width), 1280
	}
	if height > 900 {
		width, height = max(1, width*900/height), 900
	}
	picture := image.NewRGBA(image.Rect(0, 0, width, height))
	visible := false
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			source := (y*bounds.Height/height*bounds.Width + x*bounds.Width/width) * 4
			target := (y*width + x) * 4
			picture.Pix[target], picture.Pix[target+1], picture.Pix[target+2], picture.Pix[target+3] = pixels[source+2], pixels[source+1], pixels[source], 255
			visible = visible || pixels[source] != 0 || pixels[source+1] != 0 || pixels[source+2] != 0
		}
	}
	if !visible {
		return protocol.ComputerObservation{}, bounds, errors.New("窗口返回空白图像，当前驱动无法观察该窗口")
	}
	var encoded bytes.Buffer
	if err = jpeg.Encode(&encoded, picture, &jpeg.Options{Quality: 85}); err != nil {
		return protocol.ComputerObservation{}, bounds, err
	}
	if encoded.Len() > 1000000 {
		return protocol.ComputerObservation{}, bounds, errors.New("窗口图像超出传输大小限制")
	}
	return protocol.ComputerObservation{Image: base64.StdEncoding.EncodeToString(encoded.Bytes()), MimeType: "image/jpeg", Width: width, Height: height, WindowWidth: bounds.Width, WindowHeight: bounds.Height}, bounds, nil
}

// INPUT's union has eight-byte alignment on both supported Windows targets.
type winInput struct {
	Kind uint32
	_    uint32
	Data [4]uint64
}

type keyboardInput struct {
	Key, Scan uint16
	Flags     uint32
	Time      uint32
	Extra     uintptr
}

type mouseInput struct {
	X, Y  int32
	Data  uint32
	Flags uint32
	Time  uint32
	Extra uintptr
}

func keyInput(key, scan uint16, flags uint32) winInput {
	value := winInput{Kind: 1}
	*(*keyboardInput)(unsafe.Pointer(&value.Data[0])) = keyboardInput{Key: key, Scan: scan, Flags: flags}
	return value
}

func mouseEvent(x, y int32, data, flags uint32) winInput {
	value := winInput{}
	*(*mouseInput)(unsafe.Pointer(&value.Data[0])) = mouseInput{X: x, Y: y, Data: data, Flags: flags}
	return value
}

var supportedKeys = map[string]uint16{
	"ENTER": 0x0d, "TAB": 0x09, "ESCAPE": 0x1b, "BACKSPACE": 0x08, "DELETE": 0x2e,
	"LEFT": 0x25, "UP": 0x26, "RIGHT": 0x27, "DOWN": 0x28, "HOME": 0x24, "END": 0x23,
	"PAGEUP": 0x21, "PAGEDOWN": 0x22, "CONTROL": 0x11, "SHIFT": 0x10,
	"A": 0x41, "C": 0x43, "V": 0x56, "X": 0x58, "Z": 0x5a, "Y": 0x59,
	"F": 0x46, "L": 0x4c, "S": 0x53, "O": 0x4f, "P": 0x50, "W": 0x57,
}

func actionInputs(request nativeRequest) ([]winInput, error) {
	action := request.Action
	switch action.Kind {
	case "type":
		result := []winInput{}
		for _, unit := range utf16.Encode([]rune(action.Text)) {
			if unit == '\n' || unit == '\t' {
				key := uint16(0x0d)
				if unit == '\t' {
					key = 0x09
				}
				result = append(result, keyInput(key, 0, 0), keyInput(key, 0, 2))
			} else {
				result = append(result, keyInput(0, unit, 4), keyInput(0, unit, 6))
			}
		}
		return result, nil
	case "key":
		keys := []uint16{}
		for _, name := range action.Keys {
			key, ok := supportedKeys[strings.ToUpper(name)]
			if !ok {
				return nil, errors.New("该按键不在窗口控制支持范围内")
			}
			keys = append(keys, key)
		}
		result := []winInput{}
		for _, key := range keys {
			result = append(result, keyInput(key, 0, 0))
		}
		for index := len(keys) - 1; index >= 0; index-- {
			result = append(result, keyInput(keys[index], 0, 2))
		}
		return result, nil
	case "click", "scroll":
		if request.Observation.Width <= 0 || request.Observation.Height <= 0 {
			return nil, errors.New("窗口观察尺寸无效")
		}
		left, _, _ := getSystemMetrics.Call(76) // Virtual screen origin and extent.
		top, _, _ := getSystemMetrics.Call(77)
		width, _, _ := getSystemMetrics.Call(78)
		height, _, _ := getSystemMetrics.Call(79)
		if width <= 1 || height <= 1 {
			return nil, errors.New("交互桌面尺寸无效")
		}
		if action.X == nil || action.Y == nil {
			return nil, errors.New("需要明确的窗口坐标")
		}
		x := request.Bounds.Left + *action.X*request.Bounds.Width/request.Observation.Width
		y := request.Bounds.Top + *action.Y*request.Bounds.Height/request.Observation.Height
		if x < int(int32(left)) || y < int(int32(top)) || x >= int(int32(left))+int(width) || y >= int(int32(top))+int(height) {
			return nil, errors.New("目标坐标位于可见桌面之外")
		}
		point := uint64(uint32(int32(x))) | uint64(uint32(int32(y)))<<32
		hit, _, _ := windowFromPoint.Call(uintptr(point))
		root, _, _ := getAncestor.Call(hit, 2) // GA_ROOT.
		target, _, err := windowHandle(request.Window)
		if err != nil || hit == 0 || root != target {
			return nil, errors.New("目标位置被其他窗口遮挡，请重新观察")
		}
		absoluteX := int32((x - int(int32(left))) * 65535 / int(width-1))
		absoluteY := int32((y - int(int32(top))) * 65535 / int(height-1))
		result := []winInput{mouseEvent(absoluteX, absoluteY, 0, 0xc001)}
		if action.Kind == "scroll" {
			return append(result, mouseEvent(0, 0, uint32(int32(action.Scroll*120)), 0x0800)), nil
		}
		down, up := uint32(2), uint32(4)
		if action.Button == "right" {
			down, up = 8, 16
		}
		return append(result, mouseEvent(0, 0, 0, down), mouseEvent(0, 0, 0, up)), nil
	}
	return nil, errors.New("unsupported computer input")
}

func nativeAct(request nativeRequest) error {
	hwnd, bounds, err := windowHandle(request.Window)
	if err != nil {
		return err
	}
	if bounds != request.Bounds {
		return errors.New("窗口已移动或调整大小，请重新观察")
	}
	for _, key := range []uintptr{0x10, 0x11, 0x12, 0x5b, 0x5c, 1, 2} {
		state, _, _ := getAsyncKeyState.Call(key)
		if state&0x8000 != 0 {
			return errors.New("用户正在按住按键或鼠标，请松开后重新观察")
		}
	}
	setForegroundWindow.Call(hwnd)
	foreground, _, _ := getForegroundWindow.Call()
	if foreground != hwnd {
		return errors.New("请将授权窗口切到前台后重新观察")
	}
	inputs, err := actionInputs(request)
	if err != nil {
		return err
	}
	if _, _, err = windowHandle(request.Window); err != nil {
		return err
	}
	foreground, _, _ = getForegroundWindow.Call()
	if foreground != hwnd || len(inputs) == 0 {
		return errors.New("窗口焦点已变化或操作为空")
	}
	count, _, inputErr := sendInput.Call(uintptr(len(inputs)), uintptr(unsafe.Pointer(&inputs[0])), unsafe.Sizeof(winInput{}))
	if int(count) != len(inputs) {
		// Release only this operation's requested keys/buttons after a partial
		// insertion. The parent keeps the session unknown until it is reviewed.
		releases := []winInput{mouseEvent(0, 0, 0, 4), mouseEvent(0, 0, 0, 16)}
		for _, name := range request.Action.Keys {
			if key := supportedKeys[strings.ToUpper(name)]; key != 0 {
				releases = append(releases, keyInput(key, 0, 2))
			}
		}
		sendInput.Call(uintptr(len(releases)), uintptr(unsafe.Pointer(&releases[0])), unsafe.Sizeof(winInput{}))
		return fmt.Errorf("系统未确认全部窗口输入 %w", inputErr)
	}
	return nil
}
