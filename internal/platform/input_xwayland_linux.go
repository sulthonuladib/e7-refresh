//go:build linux

package platform

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
	"github.com/jezek/xgb/xtest"
)

// xInput drives one X window from inside the X server with XTEST, without
// touching the compositor's pointer.
//
// On Hyprland an XWayland client keeps its own pointer sprite: XWarpPointer
// moves it, XTEST events are delivered to the window under it, and the
// compositor never sees either. That is what makes a click land on a window
// that is hidden on another workspace while the user's cursor, focus and
// workspace stay exactly where they are. Everything here is restored in a
// deferred warp, so the sprite ends where it started.
type xInput struct {
	mu    sync.Mutex
	conn  *xgb.Conn
	root  xproto.Window
	win   xproto.Window
	title string
}

// x11Display returns the X display of the running server, setting DISPLAY
// when the process started without the graphical session's environment.
// XWayland listens on the first X socket, which is what an app launched from
// a systemd user service or a shell without session variables needs.
func x11Display() string {
	if display := os.Getenv("DISPLAY"); display != "" {
		return display
	}
	matches, err := filepath.Glob("/tmp/.X11-unix/X*")
	if err != nil || len(matches) == 0 {
		return ""
	}
	best := -1
	for _, match := range matches {
		number, err := strconv.Atoi(strings.TrimPrefix(filepath.Base(match), "X"))
		if err != nil {
			continue
		}
		if best < 0 || number < best {
			best = number
		}
	}
	if best < 0 {
		return ""
	}
	display := fmt.Sprintf(":%d", best)
	_ = os.Setenv("DISPLAY", display)
	return display
}

// newXInput connects to the X server and finds the window with this title.
// class is the Hyprland window class; matching it as well keeps the search
// from picking up an unrelated window with a similar name.
func newXInput(title, class string) (*xInput, error) {
	if x11Display() == "" {
		return nil, fmt.Errorf("no X display found (XWayland is not running)")
	}
	conn, err := openXConn()
	if err != nil {
		return nil, err
	}
	root := xproto.Setup(conn).DefaultScreen(conn).Root
	window, err := findXWindow(conn, root, title, class)
	if err != nil {
		conn.Close()
		return nil, err
	}
	return &xInput{conn: conn, root: root, win: window, title: title}, nil
}

// openXConn connects to the X server with the XTEST extension ready. A
// session without an Xauthority file is normal for Xwayland started by
// Hyprland, and xgb logs a line about it on every connection; that noise is
// kept out of the app's output, while real protocol errors still surface.
func openXConn() (*xgb.Conn, error) {
	if x11Display() == "" {
		return nil, fmt.Errorf("no X display found (Xwayland is not running)")
	}
	previous := xgb.Logger
	xgb.Logger = log.New(io.Discard, "", 0)
	conn, err := xgb.NewConn()
	xgb.Logger = previous
	if err != nil {
		return nil, fmt.Errorf("connect to X: %w", err)
	}
	if err := xtest.Init(conn); err != nil {
		conn.Close()
		return nil, fmt.Errorf("XTEST extension: %w", err)
	}
	return conn, nil
}

// probeXTEST checks that the X server in use has XTEST, without touching any
// window.
func probeXTEST() error {
	conn, err := openXConn()
	if err != nil {
		return err
	}
	conn.Close()
	return nil
}

// findXWindow walks the tree for a viewable window with this title. A game
// under Proton leaves several helper windows with the same class, so the
// largest match wins. When no title matches, the class alone is enough: the
// window title in the settings and the X title can differ by a build suffix.
func findXWindow(conn *xgb.Conn, root xproto.Window, title, class string) (xproto.Window, error) {
	type candidate struct {
		window xproto.Window
		area   int
	}
	var byTitle, byClass candidate
	var walk func(window xproto.Window, depth int)
	walk = func(window xproto.Window, depth int) {
		children, err := xproto.QueryTree(conn, window).Reply()
		if err != nil || depth > 3 {
			return
		}
		for _, child := range children.Children {
			attrs, err := xproto.GetWindowAttributes(conn, child).Reply()
			if err != nil {
				continue
			}
			viewable := attrs.MapState == xproto.MapStateViewable
			geometry, geoErr := xproto.GetGeometry(conn, xproto.Drawable(child)).Reply()
			area := 0
			if geoErr == nil {
				area = int(geometry.Width) * int(geometry.Height)
			}
			wmClass := xWindowClass(conn, child)
			classOK := class == "" || strings.Contains(wmClass, class)
			if viewable && area > 0 {
				if classOK && area > byClass.area {
					byClass = candidate{window: child, area: area}
				}
				if classOK && strings.EqualFold(strings.TrimSpace(xWindowName(conn, child)), title) && area > byTitle.area {
					byTitle = candidate{window: child, area: area}
				}
			}
			walk(child, depth+1)
		}
	}
	walk(root, 0)
	switch {
	case byTitle.window != 0:
		return byTitle.window, nil
	case byClass.window != 0:
		return byClass.window, nil
	}
	return 0, fmt.Errorf("no viewable X window titled %q (is the game running under XWayland?)", title)
}

// xWindowName reads _NET_WM_NAME, falling back to the legacy WM_NAME.
func xWindowName(conn *xgb.Conn, window xproto.Window) string {
	netName, err := xproto.InternAtom(conn, false, uint16(len("_NET_WM_NAME")), "_NET_WM_NAME").Reply()
	if err == nil {
		property, err := xproto.GetProperty(conn, false, window, netName.Atom,
			xproto.GetPropertyTypeAny, 0, 1024).Reply()
		if err == nil && len(property.Value) > 0 {
			return string(property.Value)
		}
	}
	property, err := xproto.GetProperty(conn, false, window, xproto.AtomWmName,
		xproto.GetPropertyTypeAny, 0, 1024).Reply()
	if err != nil || len(property.Value) == 0 {
		return ""
	}
	return string(property.Value)
}

// xWindowClass reads WM_CLASS, which Hyprland exposes as the client class.
func xWindowClass(conn *xgb.Conn, window xproto.Window) string {
	property, err := xproto.GetProperty(conn, false, window, xproto.AtomWmClass,
		xproto.GetPropertyTypeAny, 0, 256).Reply()
	if err != nil || len(property.Value) == 0 {
		return ""
	}
	return strings.ReplaceAll(string(property.Value), "\x00", " ")
}

// pointer returns the X pointer position, which the compositor's cursor does
// not follow: it is only Xwayland's idea of where the pointer is.
func (x *xInput) pointer() (int, int) {
	reply, err := xproto.QueryPointer(x.conn, x.root).Reply()
	if err != nil {
		return 0, 0
	}
	return int(reply.RootX), int(reply.RootY)
}

func (x *xInput) warp(dst xproto.Window, px, py int) error {
	if err := xproto.WarpPointerChecked(x.conn, 0, dst, 0, 0, 0, 0,
		int16(px), int16(py)).Check(); err != nil {
		return fmt.Errorf("warp pointer: %w", err)
	}
	x.conn.Sync()
	return nil
}

func (x *xInput) button(down bool) error {
	kind := byte(xproto.ButtonRelease)
	if down {
		kind = xproto.ButtonPress
	}
	if err := xtest.FakeInputChecked(x.conn, kind, 1, 0, x.root, 0, 0, 0).Check(); err != nil {
		return fmt.Errorf("fake button event: %w", err)
	}
	x.conn.Sync()
	return nil
}

// Click presses and releases the left button at a window-local point. The
// pointer is moved one pixel first: the game needs a motion event to register
// the hover before it accepts the press.
func (x *xInput) Click(px, py int) error {
	x.mu.Lock()
	defer x.mu.Unlock()
	startX, startY := x.pointer()
	defer func() { _ = x.warp(x.root, startX, startY) }()
	if err := x.warp(x.win, px, py); err != nil {
		return err
	}
	time.Sleep(12 * time.Millisecond)
	if err := x.warp(x.win, px+1, py); err != nil {
		return err
	}
	time.Sleep(60 * time.Millisecond)
	if err := x.button(true); err != nil {
		return err
	}
	time.Sleep(45 * time.Millisecond)
	if err := x.button(false); err != nil {
		return err
	}
	time.Sleep(20 * time.Millisecond)
	return nil
}

// Drag presses, moves in small steps and releases, which is how the shop list
// and the refresh button are scrolled.
func (x *xInput) Drag(x1, y1, x2, y2 int) error {
	x.mu.Lock()
	defer x.mu.Unlock()
	startX, startY := x.pointer()
	defer func() { _ = x.warp(x.root, startX, startY) }()
	if err := x.warp(x.win, x1, y1); err != nil {
		return err
	}
	time.Sleep(20 * time.Millisecond)
	if err := x.button(true); err != nil {
		return err
	}
	const steps = 12
	for i := 1; i <= steps; i++ {
		px := x1 + (x2-x1)*i/steps
		py := y1 + (y2-y1)*i/steps
		if err := x.warp(x.win, px, py); err != nil {
			return err
		}
		time.Sleep(15 * time.Millisecond)
	}
	time.Sleep(30 * time.Millisecond)
	return x.button(false)
}

// Close releases the X connection. The engine keeps one per run.
func (x *xInput) Close() {
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.conn != nil {
		x.conn.Close()
		x.conn = nil
	}
}
