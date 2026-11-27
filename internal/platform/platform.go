// Package platform is the small abstraction the refresh macros need from the
// desktop: find and move a window, grab pixels, send mouse events.
//
// Three implementations sit behind one interface. On Hyprland an XWayland
// window (a game under Proton, which is how Epic Seven runs) is captured and
// clicked in the background: `grim -T` renders the window on its own and
// XTEST events are injected inside Xwayland, so the user's pointer, focus and
// workspace are never touched. Everything else keeps the older path: grim or
// an X11 screenshot tool grabs the screen and xdotool/ydotool move the real
// pointer, which needs the window to be visible and focused. Windows uses the
// Win32 and GDI APIs through syscalls, with no cgo.
package platform

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Rect is a screen rectangle in physical pixels.
type Rect struct {
	X, Y, W, H int
}

// Valid reports whether a rectangle can be used for a macro.
func (r Rect) Valid() bool { return r.W > 0 && r.H > 0 }

// Window is the emulator or PC client window a mouse macro drives.
type Window struct {
	Title string
	Rect  Rect
	ID    string
	// Class is the desktop's window class, when it has one. XWayland keeps
	// several helper windows with the same title, so the class narrows the
	// search for the window that actually draws the game.
	Class string
	// StableID is the compositor's own handle for the toplevel, which
	// Hyprland exposes for `grim -T`. It addresses the window without
	// reference to where it is on screen.
	StableID string
	// XWayland marks a window that runs through Xwayland. Those keep an X
	// pointer sprite of their own, so they can be clicked without moving
	// the compositor's cursor.
	XWayland bool
}

// Desktop is everything the mouse macro asks of the operating system.
type Desktop interface {
	Kind() string
	FindWindow(title string) (*Window, error)
	Activate(w *Window) error
	MoveResize(w *Window, r Rect) error
	// Geometry returns the window's current screen rectangle. A compositor
	// that re-centres a floating window when it is resized can move it, so
	// the caller must read the result back instead of trusting the request.
	Geometry(w *Window) (Rect, error)
	Capture() (image.Image, error)
	Click(x, y int) error
	DoubleClick(x, y int, interval time.Duration) error
	Drag(x1, y1, x2, y2 int) error
	Requirements() []Requirement
	// Background reports whether the window found by FindWindow can be
	// captured and clicked while it stays hidden and unfocused. When it is
	// true the caller must not focus, raise or move the window, and the
	// geometry, pixels and click coordinates are all window-local: the
	// rectangle starts at 0,0 and the capture is the window itself.
	Background() bool
}

// Requirement names an external tool the current desktop needs.
type Requirement struct {
	Tool    string
	Why     string
	Install string
}

// Missing returns the requirements whose executable is not on PATH.
func Missing(reqs []Requirement) []Requirement {
	var missing []Requirement
	seen := map[string]bool{}
	for _, r := range reqs {
		if seen[r.Tool] {
			continue
		}
		seen[r.Tool] = true
		if _, err := exec.LookPath(r.Tool); err != nil {
			missing = append(missing, r)
		}
	}
	return missing
}

// FormatRequirements turns missing tools into one readable message.
func FormatRequirements(reqs []Requirement) string {
	if len(reqs) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Missing tools for this desktop:\n")
	for _, r := range reqs {
		fmt.Fprintf(&b, "  %s - %s\n    install: %s\n", r.Tool, r.Why, r.Install)
	}
	return strings.TrimRight(b.String(), "\n")
}

// Run captures the output of a helper process with a bounded lifetime.
func Run(timeout time.Duration, name string, args ...string) ([]byte, error) {
	ctx := context.Background()
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if os.Getenv("E7_REFRESH_DEBUG") != "" {
			log.Printf("platform: %s %v failed: %v stderr=%q", name, args, err, stderr.String())
		}
		detail := strings.TrimSpace(stderr.String())
		if len(detail) > 400 {
			detail = detail[:400]
		}
		if detail != "" {
			return nil, fmt.Errorf("%s: %w: %s", name, err, detail)
		}
		return nil, err
	}
	return stdout.Bytes(), nil
}
