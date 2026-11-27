//go:build windows

package platform

import (
	"fmt"
	"image"
	"os"
	"strconv"
	"sync"
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
	procGetClientRect        = user32.NewProc("GetClientRect")
	procClientToScreen       = user32.NewProc("ClientToScreen")
	procIsIconic             = user32.NewProc("IsIconic")
	procShowWindow           = user32.NewProc("ShowWindow")
	procSetForegroundWindow  = user32.NewProc("SetForegroundWindow")
	procMoveWindow           = user32.NewProc("MoveWindow")
	procSetWindowPos         = user32.NewProc("SetWindowPos")
	procSetCursorPos         = user32.NewProc("SetCursorPos")
	procMouseEvent           = user32.NewProc("mouse_event")
	procGetDC                = user32.NewProc("GetDC")
	procReleaseDC            = user32.NewProc("ReleaseDC")
	procGetSystemMetrics     = user32.NewProc("GetSystemMetrics")
	// PrintWindow asks the window to draw itself into a device context, which
	// works for a window that is covered or on another virtual desktop.
	procPrintWindow  = user32.NewProc("PrintWindow")
	procPostMessageW = user32.NewProc("PostMessageW")

	procCreateCompatibleDC     = gdi32.NewProc("CreateCompatibleDC")
	procCreateCompatibleBitmap = gdi32.NewProc("CreateCompatibleBitmap")
	procSelectObject           = gdi32.NewProc("SelectObject")
	procBitBlt                 = gdi32.NewProc("BitBlt")
	procGetDIBits              = gdi32.NewProc("GetDIBits")
	procDeleteObject           = gdi32.NewProc("DeleteObject")
	procDeleteDC               = gdi32.NewProc("DeleteDC")
)

const (
	swRestore       = 9
	sxVirtualScreen = 76
	syVirtualScreen = 77
	cxVirtualScreen = 78
	cyVirtualScreen = 79
	srcCopy         = 0x00CC0020
	captureBlt      = 0x08000000
	leftDown        = 0x0002
	leftUp          = 0x0004

	// PrintWindow flags: PW_CLIENTONLY is unreliable across toolkits, so the
	// whole window is rendered and the client area is cropped out of it.
	pwRenderFullContent = 0x00000002

	swpNoZOrder   = 0x0004
	swpNoActivate = 0x0010

	// Posted once per run when E7_REFRESH_ACTIVATE is set, for games that
	// check whether they are the active window before handling input.
	wmActivate = 0x0006
	wmSetFocus = 0x0007
	waActive   = 1

	wmMouseMove   = 0x0200
	wmLButtonDown = 0x0201
	wmLButtonUp   = 0x0202
	mkLButton     = 0x0001
)

type rect struct {
	Left, Top, Right, Bottom int32
}

type point struct {
	X, Y int32
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

type windowsDesktop struct {
	mu sync.Mutex
	// target is the window FindWindow settled on, and background says
	// whether it can be captured and clicked while the user works: it is set
	// once, from an actual PrintWindow render, and off when
	// E7_REFRESH_FOREGROUND is set.
	target     *Window
	background bool
	probed     bool
	activates  sync.Once
}

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
	window := &Window{
		Title: title,
		ID:    strconv.FormatUint(uint64(hwndFound), 10),
		Rect: Rect{
			X: int(found.Left),
			Y: int(found.Top),
			W: int(found.Right - found.Left),
			H: int(found.Bottom - found.Top),
		},
	}
	d.mu.Lock()
	d.target = window
	d.probed = false
	d.background = false
	d.mu.Unlock()
	d.detectBackground()
	return window, nil
}

// detectBackground renders the window once. If PrintWindow gives no pixels
// at all, the old path is the only one that works, and saying so is better
// than capturing an empty window and matching nothing.
func (d *windowsDesktop) detectBackground() {
	d.mu.Lock()
	target, probed := d.target, d.probed
	d.mu.Unlock()
	if target == nil || probed {
		return
	}
	ok := false
	defer func() {
		d.mu.Lock()
		d.probed = true
		d.background = ok
		d.mu.Unlock()
	}()
	if os.Getenv("E7_REFRESH_FOREGROUND") != "" {
		return
	}
	hwnd, err := handle(target)
	if err != nil {
		return
	}
	if iconic, _, _ := procIsIconic.Call(hwnd); iconic != 0 {
		// A minimized window has no client area to render.
		return
	}
	frame, err := captureClient(hwnd)
	if err != nil {
		return
	}
	ok = hasContent(frame)
}

// hasContent reports whether a render carries a real picture. PrintWindow
// returns an all-black bitmap for windows that refuse to render themselves,
// which is the failure this guards against.
func hasContent(frame image.Image) bool {
	bounds := frame.Bounds()
	total := bounds.Dx() * bounds.Dy()
	if total == 0 {
		return false
	}
	var lit int
	for y := bounds.Min.Y; y < bounds.Max.Y; y += 2 {
		for x := bounds.Min.X; x < bounds.Max.X; x += 2 {
			r, g, b, _ := frame.At(x, y).RGBA()
			if r|g|b != 0 {
				lit++
			}
		}
	}
	return lit*100 >= (total / 4) // at least 1% of the sampled pixels
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

// Background reports whether the game can be captured and clicked while it
// stays behind other windows.
func (d *windowsDesktop) Background() bool {
	d.detectBackground()
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.background
}

func (d *windowsDesktop) Activate(w *Window) error {
	if d.Background() {
		// Raising or focusing the game would take the user's keyboard away,
		// which is the whole point of the background path.
		return nil
	}
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
	if d.Background() {
		// Keep the window where it is; only the client area has to be the
		// size the templates expect. The frame adds a border and a title bar,
		// so the window has to grow by that difference.
		client := clientSize(hwnd)
		frame := windowSize(hwnd)
		if client.W == r.W && client.H == r.H {
			return nil
		}
		extraW, extraH := frame.W-client.W, frame.H-client.H
		procSetWindowPos.Call(hwnd, 0, 0, 0, uintptr(r.W+extraW), uintptr(r.H+extraH),
			swpNoZOrder|swpNoActivate)
		return nil
	}
	procMoveWindow.Call(hwnd, uintptr(r.X), uintptr(r.Y), uintptr(r.W), uintptr(r.H), 1)
	return nil
}

func windowSize(hwnd uintptr) Rect {
	var found rect
	procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&found)))
	return Rect{W: int(found.Right - found.Left), H: int(found.Bottom - found.Top)}
}

func clientSize(hwnd uintptr) Rect {
	var found rect
	procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&found)))
	return Rect{W: int(found.Right - found.Left), H: int(found.Bottom - found.Top)}
}

// Geometry reads the window rectangle back, since MoveWindow can clamp a
// window that would extend past the edge of the desktop. In the background
// path the capture is the client area, so the rectangle starts at 0,0.
func (d *windowsDesktop) Geometry(w *Window) (Rect, error) {
	hwnd, err := handle(w)
	if err != nil {
		return Rect{}, err
	}
	if d.Background() {
		client := clientSize(hwnd)
		return Rect{X: 0, Y: 0, W: client.W, H: client.H}, nil
	}
	var found rect
	procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&found)))
	return Rect{
		X: int(found.Left), Y: int(found.Top),
		W: int(found.Right - found.Left), H: int(found.Bottom - found.Top),
	}, nil
}

func (d *windowsDesktop) Capture() (image.Image, error) {
	d.mu.Lock()
	target := d.target
	d.mu.Unlock()
	if d.Background() && target != nil {
		hwnd, err := handle(target)
		if err != nil {
			return nil, err
		}
		frame, err := captureClient(hwnd)
		if err != nil {
			return nil, fmt.Errorf("capture window: %w", err)
		}
		return frame, nil
	}
	return captureScreen()
}

// captureClient renders the window with PrintWindow and returns its client
// area, which is the coordinate space WM_ mouse messages use.
func captureClient(hwnd uintptr) (image.Image, error) {
	frame := windowSize(hwnd)
	client := clientSize(hwnd)
	if frame.W <= 0 || frame.H <= 0 || client.W <= 0 || client.H <= 0 {
		return nil, fmt.Errorf("window has no area to capture")
	}
	origin := point{}
	procClientToScreen.Call(hwnd, uintptr(unsafe.Pointer(&origin)))
	var bounds rect
	procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&bounds)))
	offX, offY := int(origin.X-bounds.Left), int(origin.Y-bounds.Top)

	screen, _, _ := procGetDC.Call(0)
	if screen == 0 {
		return nil, fmt.Errorf("GetDC failed")
	}
	defer procReleaseDC.Call(0, screen)
	memDC, _, _ := procCreateCompatibleDC.Call(screen)
	if memDC == 0 {
		return nil, fmt.Errorf("CreateCompatibleDC failed")
	}
	defer procDeleteDC.Call(memDC)
	bitmap, _, _ := procCreateCompatibleBitmap.Call(screen, uintptr(frame.W), uintptr(frame.H))
	if bitmap == 0 {
		return nil, fmt.Errorf("CreateCompatibleBitmap failed")
	}
	defer procDeleteObject.Call(bitmap)
	old, _, _ := procSelectObject.Call(memDC, bitmap)
	defer procSelectObject.Call(memDC, old)
	if rendered, _, _ := procPrintWindow.Call(hwnd, memDC, pwRenderFullContent); rendered == 0 {
		return nil, fmt.Errorf("PrintWindow failed")
	}
	pixels, err := readPixels(memDC, bitmap, frame.W, frame.H)
	if err != nil {
		return nil, err
	}
	// Crop the client area out of the rendered frame.
	out := image.NewRGBA(image.Rect(0, 0, client.W, client.H))
	for row := 0; row < client.H; row++ {
		srcRow, srcCol := row+offY, offX
		if srcRow < 0 || srcRow >= frame.H || srcCol < 0 || srcCol+client.W > frame.W {
			break
		}
		src := srcRow*frame.W*4 + srcCol*4
		dst := row * out.Stride
		for col := 0; col < client.W; col++ {
			copy(out.Pix[dst+col*4:dst+col*4+4], pixels[src+col*4:src+col*4+4])
		}
	}
	return out, nil
}

// readPixels copies a device context into top-down RGBA bytes, undoing the
// bottom-up order GetDIBits uses.
func readPixels(memDC, bitmap uintptr, w, h int) ([]byte, error) {
	info := bitmapInfo{Header: bitmapInfoHeader{
		Size:     uint32(unsafe.Sizeof(bitmapInfoHeader{})),
		Width:    int32(w),
		Height:   int32(h),
		Planes:   1,
		BitCount: 32,
	}}
	raw := make([]byte, w*h*4)
	ok, _, _ := procGetDIBits.Call(memDC, bitmap, 0, uintptr(h),
		uintptr(unsafe.Pointer(&raw[0])), uintptr(unsafe.Pointer(&info)), 0)
	if ok == 0 {
		return nil, fmt.Errorf("GetDIBits failed")
	}
	pixels := make([]byte, w*h*4)
	for row := 0; row < h; row++ {
		src := (h - 1 - row) * w * 4
		dst := row * w * 4
		for col := 0; col < w; col++ {
			s, d := src+col*4, dst+col*4
			pixels[d] = raw[s+2]   // red
			pixels[d+1] = raw[s+1] // green
			pixels[d+2] = raw[s]   // blue
			pixels[d+3] = raw[s+3] // alpha
		}
	}
	return pixels, nil
}

func captureScreen() (image.Image, error) {
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
	pixels, err := readPixels(memDC, bitmap, w, h)
	if err != nil {
		return nil, err
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for row := 0; row < h; row++ {
		copy(img.Pix[row*img.Stride:row*img.Stride+w*4], pixels[row*w*4:(row+1)*w*4])
	}
	return img, nil
}

// postMouse sends a mouse message straight to the window's message queue. The
// system cursor and the focused window are untouched, which is the Windows
// counterpart of the XTEST path: no SendInput, no SetCursorPos, no activation.
func (d *windowsDesktop) postMouse(hwnd uintptr, message uintptr, wParam uintptr, x, y int) error {
	lParam := uintptr(uint32(uint16(x)) | uint32(uint16(y))<<16)
	if ok, _, err := procPostMessageW.Call(hwnd, message, wParam, lParam); ok == 0 {
		return fmt.Errorf("PostMessage %#x failed: %w", message, err)
	}
	return nil
}

// nudgeActivation posts WM_ACTIVATE and WM_SETFOCUS so that a game which
// checks GetActiveWindow before handling input believes it is active. The
// real focus does not move: these are messages in the window's own queue.
// Off by default, because a toolkit that trusts them can paint itself as
// focused while the user is typing somewhere else.
func (d *windowsDesktop) nudgeActivation(hwnd uintptr) {
	if os.Getenv("E7_REFRESH_ACTIVATE") == "" {
		return
	}
	d.activates.Do(func() {
		procPostMessageW.Call(hwnd, wmActivate, waActive, 0)
		procPostMessageW.Call(hwnd, wmSetFocus, 0, 0)
		time.Sleep(20 * time.Millisecond)
	})
}

// clickBackground clicks a window-local point without touching the cursor.
// A motion message goes first: games track hover from it, and a press with no
// preceding move is often dropped as a stray event.
func (d *windowsDesktop) clickBackground(hwnd uintptr, x, y int) error {
	d.nudgeActivation(hwnd)
	if err := d.postMouse(hwnd, wmMouseMove, 0, x, y); err != nil {
		return err
	}
	time.Sleep(12 * time.Millisecond)
	if err := d.postMouse(hwnd, wmMouseMove, 0, x+1, y); err != nil {
		return err
	}
	time.Sleep(60 * time.Millisecond)
	if err := d.postMouse(hwnd, wmLButtonDown, mkLButton, x, y); err != nil {
		return err
	}
	time.Sleep(45 * time.Millisecond)
	return d.postMouse(hwnd, wmLButtonUp, 0, x, y)
}

func (d *windowsDesktop) Click(x, y int) error {
	if d.Background() {
		d.mu.Lock()
		target := d.target
		d.mu.Unlock()
		hwnd, err := handle(target)
		if err != nil {
			return err
		}
		return d.clickBackground(hwnd, x, y)
	}
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
	if d.Background() {
		d.mu.Lock()
		target := d.target
		d.mu.Unlock()
		hwnd, err := handle(target)
		if err != nil {
			return err
		}
		d.nudgeActivation(hwnd)
		if err := d.postMouse(hwnd, wmMouseMove, 0, x1, y1); err != nil {
			return err
		}
		time.Sleep(20 * time.Millisecond)
		if err := d.postMouse(hwnd, wmLButtonDown, mkLButton, x1, y1); err != nil {
			return err
		}
		const steps = 12
		for i := 1; i <= steps; i++ {
			px := x1 + (x2-x1)*i/steps
			py := y1 + (y2-y1)*i/steps
			if err := d.postMouse(hwnd, wmMouseMove, mkLButton, px, py); err != nil {
				return err
			}
			time.Sleep(15 * time.Millisecond)
		}
		time.Sleep(30 * time.Millisecond)
		return d.postMouse(hwnd, wmLButtonUp, 0, x2, y2)
	}
	procSetCursorPos.Call(uintptr(x1), uintptr(y1))
	procMouseEvent.Call(leftDown, 0, 0, 0, 0)
	time.Sleep(50 * time.Millisecond)
	procSetCursorPos.Call(uintptr(x2), uintptr(y2))
	time.Sleep(50 * time.Millisecond)
	procMouseEvent.Call(leftUp, 0, 0, 0, 0)
	return nil
}

func (d *windowsDesktop) Requirements() []Requirement { return nil }

// EnsureSessionEnv is a no-op on Windows, where the window station and desktop
// come from the process's own session.
func EnsureSessionEnv() {}

// BackgroundCapability reports whether a refresh can capture and click the
// game while the user works. On Windows it depends on the app: the window has
// to render itself for PrintWindow and to read posted mouse messages, which
// is probed per window at run time.
func BackgroundCapability() (bool, string) {
	return true, "captures the window with PrintWindow and posts mouse messages; works when the game reads them"
}
