//go:build windows

package platform

import (
	"fmt"
	"image"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

var (
	user32 = syscall.NewLazyDLL("user32.dll")
	gdi32  = syscall.NewLazyDLL("gdi32.dll")

	procEnumWindows          = user32.NewProc("EnumWindows")
	procGetWindowTextLengthW = user32.NewProc("GetWindowTextLengthW")
	procGetWindowTextW       = user32.NewProc("GetWindowTextW")
	procIsWindowVisible      = user32.NewProc("IsWindowVisible")
	procGetWindowRect        = user32.NewProc("GetWindowRect")
	procShowWindow           = user32.NewProc("ShowWindow")
	procSetForegroundWindow  = user32.NewProc("SetForegroundWindow")
	procMoveWindow           = user32.NewProc("MoveWindow")
	procSetCursorPos         = user32.NewProc("SetCursorPos")
	procMouseEvent           = user32.NewProc("mouse_event")
	procGetAsyncKeyState     = user32.NewProc("GetAsyncKeyState")
	procGetDC                = user32.NewProc("GetDC")
	procReleaseDC            = user32.NewProc("ReleaseDC")
	procGetSystemMetrics     = user32.NewProc("GetSystemMetrics")

	procCreateCompatibleDC    = gdi32.NewProc("CreateCompatibleDC")
	procCreateCompatibleBitmap = gdi32.NewProc("CreateCompatibleBitmap")
	procSelectObject          = gdi32.NewProc("SelectObject")
	procBitBlt                = gdi32.NewProc("BitBlt")
	procGetDIBits             = gdi32.NewProc("GetDIBits")
	procDeleteObject          = gdi32.NewProc("DeleteObject")
	procDeleteDC              = gdi32.NewProc("DeleteDC")
)

const (
	swRestore      = 9
	sxVirtualScreen = 76
	syVirtualScreen = 77
	cxVirtualScreen = 78
	cyVirtualScreen = 79
	srcCopy         = 0x00CC0020
	captureBlt      = 0x08000000
	leftDown        = 0x0002
	leftUp          = 0x0004
)

type rect struct {
	Left, Top, Right, Bottom int32
}

type rgbQuad struct {
	Blue, Green, Red, Reserved byte
}

type bitmapInfoHeader struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

type bitmapInfo struct {
	Header bitmapInfoHeader
	Colors [1]rgbQuad
}

type windowsDesktop struct{}

// New returns the Win32 desktop implementation.
func New() Desktop { return &windowsDesktop{} }

func (d *windowsDesktop) Kind() string { return "windows" }

func (d *windowsDesktop) FindWindow(title string) (*Window, error) {
	var found rect
	var hwndFound uintptr
	callback := syscall.NewCallback(func(hwnd, _ uintptr) uintptr {
		visible, _, _ := procIsWindowVisible.Call(hwnd)
		if visible == 0 {
			return 1
		}
		length, _, _ := procGetWindowTextLengthW.Call(hwnd)
		if length == 0 {
			return 1
		}
		buffer := make([]uint16, length+1)
		procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buffer[0])), length+1)
		if syscall.UTF16ToString(buffer) != title {
			return 1
		}
		var r rect
		procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
		found, hwndFound = r, hwnd
		return 0
	})
	procEnumWindows.Call(callback, 0)
	if hwndFound == 0 {
		return nil, fmt.Errorf("no visible window titled %q", title)
	}
	return &Window{
		Title: title,
		ID:    strconv.FormatUint(uint64(hwndFound), 10),
		Rect: Rect{
			X: int(found.Left),
			Y: int(found.Top),
			W: int(found.Right - found.Left),
			H: int(found.Bottom - found.Top),
		},
	}, nil
}

func handle(w *Window) (uintptr, error) {
	if w == nil || w.ID == "" {
		return 0, fmt.Errorf("no window handle")
	}
	n, err := strconv.ParseUint(w.ID, 10, 64)
	if err != nil {
		return 0, err
	}
	return uintptr(n), nil
}

func (d *windowsDesktop) Activate(w *Window) error {
	hwnd, err := handle(w)
	if err != nil {
		return err
	}
	procShowWindow.Call(hwnd, swRestore)
	procSetForegroundWindow.Call(hwnd)
	return nil
}

func (d *windowsDesktop) MoveResize(w *Window, r Rect) error {
	hwnd, err := handle(w)
	if err != nil {
		return err
	}
	procMoveWindow.Call(hwnd, uintptr(r.X), uintptr(r.Y), uintptr(r.W), uintptr(r.H), 1)
	return nil
}

func (d *windowsDesktop) Capture() (image.Image, error) {
	xr, _, _ := procGetSystemMetrics.Call(sxVirtualScreen)
	yr, _, _ := procGetSystemMetrics.Call(syVirtualScreen)
	wr, _, _ := procGetSystemMetrics.Call(cxVirtualScreen)
	hr, _, _ := procGetSystemMetrics.Call(cyVirtualScreen)
	x, y, w, h := int(xr), int(yr), int(wr), int(hr)
	if w <= 0 || h <= 0 {
		return nil, fmt.Errorf("cannot determine virtual screen size")
	}
	hdc, _, _ := procGetDC.Call(0)
	if hdc == 0 {
		return nil, fmt.Errorf("GetDC failed")
	}
	defer procReleaseDC.Call(0, hdc)
	memDC, _, _ := procCreateCompatibleDC.Call(hdc)
	if memDC == 0 {
		return nil, fmt.Errorf("CreateCompatibleDC failed")
	}
	defer procDeleteDC.Call(memDC)
	bitmap, _, _ := procCreateCompatibleBitmap.Call(hdc, uintptr(w), uintptr(h))
	if bitmap == 0 {
		return nil, fmt.Errorf("CreateCompatibleBitmap failed")
	}
	defer procDeleteObject.Call(bitmap)
	old, _, _ := procSelectObject.Call(memDC, bitmap)
	defer procSelectObject.Call(memDC, old)
	result, _, _ := procBitBlt.Call(memDC, 0, 0, uintptr(w), uintptr(h), hdc,
		uintptr(x), uintptr(y), srcCopy|captureBlt)
	if result == 0 {
		return nil, fmt.Errorf("BitBlt failed")
	}
	info := bitmapInfo{Header: bitmapInfoHeader{
		Size:    uint32(unsafe.Sizeof(bitmapInfoHeader{})),
		Width:   int32(w),
		Height:  int32(h),
		Planes:  1,
		BitCount: 32,
	}}
	pixels := make([]byte, w*h*4)
	ok, _, _ := procGetDIBits.Call(memDC, bitmap, 0, uintptr(h),
		uintptr(unsafe.Pointer(&pixels[0])), uintptr(unsafe.Pointer(&info)), 0)
	if ok == 0 {
		return nil, fmt.Errorf("GetDIBits failed")
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for row := 0; row < h; row++ {
		src := (h - 1 - row) * w * 4 // GetDIBits returns bottom-up rows
		dst := row * img.Stride
		for col := 0; col < w; col++ {
			s, d := src+col*4, dst+col*4
			img.Pix[d] = pixels[s+2]     // red
			img.Pix[d+1] = pixels[s+1]   // green
			img.Pix[d+2] = pixels[s]     // blue
			img.Pix[d+3] = pixels[s+3]   // alpha
		}
	}
	return img, nil
}

func (d *windowsDesktop) Click(x, y int) error {
	procSetCursorPos.Call(uintptr(x), uintptr(y))
	procMouseEvent.Call(leftDown, 0, 0, 0, 0)
	time.Sleep(20 * time.Millisecond)
	procMouseEvent.Call(leftUp, 0, 0, 0, 0)
	return nil
}

func (d *windowsDesktop) DoubleClick(x, y int, interval time.Duration) error {
	if err := d.Click(x, y); err != nil {
		return err
	}
	time.Sleep(interval)
	return d.Click(x, y)
}

func (d *windowsDesktop) Drag(x1, y1, x2, y2 int) error {
	procSetCursorPos.Call(uintptr(x1), uintptr(y1))
	procMouseEvent.Call(leftDown, 0, 0, 0, 0)
	time.Sleep(50 * time.Millisecond)
	procSetCursorPos.Call(uintptr(x2), uintptr(y2))
	time.Sleep(50 * time.Millisecond)
	procMouseEvent.Call(leftUp, 0, 0, 0, 0)
	return nil
}

func (d *windowsDesktop) KeyPressed(key string) bool {
	vks := map[string]uintptr{
		"escape": 0x1B, "esc": 0x1B, "a": 0x41, "s": 0x53, "q": 0x51,
	}
	vk, ok := vks[strings.ToLower(key)]
	if !ok {
		return false
	}
	state, _, _ := procGetAsyncKeyState.Call(vk)
	return state&0x8000 != 0
}

func (d *windowsDesktop) Requirements() []Requirement { return nil }
