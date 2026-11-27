//go:build linux

package platform

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type linuxDesktop struct {
	session    string // "x11" or "wayland"
	hyprland   bool
	screenshot string // first available screenshot program
	input      string // first available input program

	// target is the window FindWindow settled on. Hyprland can address it
	// directly, so capture and clicks do not have to go through the screen.
	target *Window

	// foreground keeps the old pointer-moving path, for when a user asks for
	// it. Background control is the default on Hyprland.
	foreground bool

	xOnce sync.Once
	xIn   *xInput
	xErr  error
}

// New returns the desktop implementation matching the current session.
//
// A systemd user service and a shell started outside the graphical session
// often have none of the session variables set. In that case the compositor
// socket under /run/user/<uid> names the session, so the daemon still finds
// Hyprland's IPC and grim instead of falling back to X11 tools that are not
// there.
func New() Desktop {
	signature := hyprlandSignature()
	wayland := waylandDisplay()
	session := strings.ToLower(os.Getenv("XDG_SESSION_TYPE"))
	switch {
	case session == "wayland", session == "x11":
	case wayland != "":
		session = "wayland"
	case os.Getenv("DISPLAY") != "":
		session = "x11"
	case signature != "":
		session = "wayland"
	default:
		session = "x11"
	}
	if session != "wayland" {
		session = "x11"
	}
	d := &linuxDesktop{
		session:    session,
		hyprland:   signature != "",
		foreground: os.Getenv("E7_REFRESH_FOREGROUND") != "",
	}
	if session == "wayland" {
		d.screenshot = firstAvailable("grim", "gnome-screenshot", "spectacle", "scrot", "import")
		d.input = firstAvailable("ydotool", "xdotool")
	} else {
		d.screenshot = firstAvailable("import", "scrot", "maim", "xwd")
		d.input = firstAvailable("xdotool", "ydotool")
	}
	return d
}

// hyprlandSignature returns the running compositor's instance signature,
// filling in HYPRLAND_INSTANCE_SIGNATURE when the process was started
// without the graphical session's environment.
func hyprlandSignature() string {
	if signature := os.Getenv("HYPRLAND_INSTANCE_SIGNATURE"); signature != "" {
		return signature
	}
	runtimeDir := os.Getenv("XDG_RUNTIME_DIR")
	if runtimeDir == "" {
		runtimeDir = fmt.Sprintf("/run/user/%d", os.Getuid())
	}
	entries, err := os.ReadDir(filepath.Join(runtimeDir, "hypr"))
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		if entry.IsDir() {
			signature := entry.Name()
			_ = os.Setenv("HYPRLAND_INSTANCE_SIGNATURE", signature)
			return signature
		}
	}
	return ""
}

// waylandDisplay returns the session's Wayland socket, setting
// WAYLAND_DISPLAY for grim and the other helpers when it is missing.
func waylandDisplay() string {
	if display := os.Getenv("WAYLAND_DISPLAY"); display != "" {
		return display
	}
	runtimeDir := os.Getenv("XDG_RUNTIME_DIR")
	if runtimeDir == "" {
		runtimeDir = fmt.Sprintf("/run/user/%d", os.Getuid())
	}
	entries, err := os.ReadDir(runtimeDir)
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, "wayland-") && !strings.HasSuffix(name, ".lock") {
			_ = os.Setenv("WAYLAND_DISPLAY", name)
			return name
		}
	}
	return ""
}

func (d *linuxDesktop) Kind() string { return d.session }

// BackgroundCapability reports whether this desktop can capture and click a
// window that is hidden and unfocused, and says how (or why not). The bar
// panel shows it, so "Host mouse" is not mistaken for "moves my cursor".
func BackgroundCapability() (bool, string) {
	if signature := hyprlandSignature(); signature == "" {
		return false, "needs Hyprland: other desktops move the pointer"
	}
	if firstAvailable("grim") == "" {
		return false, "needs grim for window capture"
	}
	if err := probeXTEST(); err != nil {
		return false, err.Error()
	}
	return true, "captures with grim -T and clicks inside Xwayland; cursor, focus and workspace stay put"
}

// Background reports whether the window found by FindWindow can be captured
// and clicked while it stays hidden and unfocused.
func (d *linuxDesktop) Background() bool { return d.backgroundWindow() != nil }

// backgroundWindow returns the target when the background path applies: a
// Wayland session on Hyprland, grim for capture, and a window that runs
// through Xwayland, which keeps the X pointer sprite XTEST needs.
func (d *linuxDesktop) backgroundWindow() *Window {
	if d.foreground || d.session != "wayland" || !d.hyprland || d.screenshot != "grim" {
		return nil
	}
	w := d.target
	if w == nil || w.StableID == "" || !w.XWayland {
		return nil
	}
	if _, err := d.xwaylandInput(); err != nil {
		return nil
	}
	return w
}

// EnsureSessionEnv fills in the graphical session variables for processes
// that were started from a shell which never inherited them: a kero/tmux
// pane, a systemd unit, a bare login shell. The GUI needs WAYLAND_DISPLAY or
// DISPLAY before it creates a window, and the service needs them to capture
// and click the game.
func EnsureSessionEnv() {
	if os.Getenv("XDG_RUNTIME_DIR") == "" {
		_ = os.Setenv("XDG_RUNTIME_DIR", fmt.Sprintf("/run/user/%d", os.Getuid()))
	}
	if os.Getenv("WAYLAND_DISPLAY") == "" {
		waylandDisplay()
	}
	if os.Getenv("DISPLAY") == "" {
		x11Display()
	}
}

// xwaylandInput opens the XTEST channel to the game window once per desktop.
// The connection is reused for the whole run: the per-click cost is then a
// warp plus two button events.
func (d *linuxDesktop) xwaylandInput() (*xInput, error) {
	d.xOnce.Do(func() {
		w := d.target
		if w == nil {
			d.xErr = fmt.Errorf("no window found yet")
			return
		}
		d.xIn, d.xErr = newXInput(w.Title, w.Class)
	})
	return d.xIn, d.xErr
}

// Close releases the X connection held for background input.
func (d *linuxDesktop) Close() error {
	if d.xIn != nil {
		d.xIn.Close()
		d.xIn = nil
	}
	return nil
}

func firstAvailable(names ...string) string {
	for _, name := range names {
		if _, err := os.Stat("/usr/bin/" + name); err == nil {
			return name
		}
		if paths := os.Getenv("PATH"); paths != "" {
			for _, dir := range filepath.SplitList(paths) {
				if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
					return name
				}
			}
		}
	}
	return ""
}

func (d *linuxDesktop) FindWindow(title string) (*Window, error) {
	if d.session == "wayland" && d.hyprland {
		return d.findHyprlandWindow(title)
	}
	if d.input == "xdotool" {
		return d.findX11Window(title)
	}
	// A Wayland compositor with no window IPC cannot let one app move or
	// focus another. Fall back to absolute coordinates; the user focuses
	// the emulator themselves and the macros still work.
	rect, err := fallbackRect()
	if err != nil {
		return nil, err
	}
	return &Window{Title: title, Rect: rect, ID: "absolute"}, nil
}

func (d *linuxDesktop) findX11Window(title string) (*Window, error) {
	out, err := Run(5*time.Second, "xdotool", "search", "--onlyvisible", "--name", title)
	if err != nil {
		return nil, fmt.Errorf("xdotool search: %w", err)
	}
	for _, id := range strings.Fields(string(out)) {
		name, err := Run(2*time.Second, "xdotool", "getwindowname", id)
		if err != nil || strings.TrimSpace(string(name)) != title {
			continue
		}
		geometry, err := Run(2*time.Second, "xdotool", "getwindowgeometry", "--shell", id)
		if err != nil {
			return nil, fmt.Errorf("xdotool getwindowgeometry: %w", err)
		}
		rect := Rect{}
		for _, line := range strings.Split(string(geometry), "\n") {
			key, value, ok := strings.Cut(line, "=")
			if !ok {
				continue
			}
			n, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil {
				continue
			}
			switch key {
			case "X":
				rect.X = n
			case "Y":
				rect.Y = n
			case "WIDTH":
				rect.W = n
			case "HEIGHT":
				rect.H = n
			}
		}
		if !rect.Valid() {
			return nil, fmt.Errorf("window %q has no usable geometry", title)
		}
		return &Window{Title: title, Rect: rect, ID: id}, nil
	}
	return nil, fmt.Errorf("no visible window titled %q", title)
}

func (d *linuxDesktop) findHyprlandWindow(title string) (*Window, error) {
	out, err := Run(5*time.Second, "hyprctl", "-j", "clients")
	if err != nil {
		return nil, fmt.Errorf("hyprctl clients: %w", err)
	}
	var clients []struct {
		Title    string `json:"title"`
		Class    string `json:"class"`
		Address  string `json:"address"`
		StableID string `json:"stableId"`
		XWayland bool   `json:"xwayland"`
		At       []int  `json:"at"`
		Size     []int  `json:"size"`
	}
	if err := json.Unmarshal(out, &clients); err != nil {
		return nil, fmt.Errorf("hyprctl clients JSON: %w", err)
	}
	for _, c := range clients {
		if c.Title != title || len(c.At) < 2 || len(c.Size) < 2 {
			continue
		}
		w := &Window{
			Title:    title,
			Class:    c.Class,
			ID:       c.Address,
			StableID: c.StableID,
			XWayland: c.XWayland,
			Rect:     Rect{X: c.At[0], Y: c.At[1], W: c.Size[0], H: c.Size[1]},
		}
		d.target = w
		return w, nil
	}
	return nil, fmt.Errorf("no visible window titled %q", title)
}

// hyprGeometry reads one window's rectangle back from the compositor.
func (d *linuxDesktop) hyprGeometry(address string) (Rect, error) {
	out, err := Run(5*time.Second, "hyprctl", "-j", "clients")
	if err != nil {
		return Rect{}, err
	}
	var clients []struct {
		Address string `json:"address"`
		At      []int  `json:"at"`
		Size    []int  `json:"size"`
	}
	if err := json.Unmarshal(out, &clients); err != nil {
		return Rect{}, err
	}
	for _, c := range clients {
		if c.Address == address && len(c.At) >= 2 && len(c.Size) >= 2 {
			return Rect{X: c.At[0], Y: c.At[1], W: c.Size[0], H: c.Size[1]}, nil
		}
	}
	return Rect{}, fmt.Errorf("window %s is gone", address)
}

func fallbackRect() (Rect, error) {
	if value := os.Getenv("E7_REFRESH_WINDOW_RECT"); value != "" {
		parts := strings.Split(value, ",")
		if len(parts) != 4 {
			return Rect{}, fmt.Errorf("E7_REFRESH_WINDOW_RECT must be x,y,w,h")
		}
		var r Rect
		for i, part := range parts {
			n, err := strconv.Atoi(strings.TrimSpace(part))
			if err != nil {
				return Rect{}, fmt.Errorf("E7_REFRESH_WINDOW_RECT: %w", err)
			}
			switch i {
			case 0:
				r.X = n
			case 1:
				r.Y = n
			case 2:
				r.W = n
			case 3:
				r.H = n
			}
		}
		if r.Valid() {
			return r, nil
		}
	}
	return Rect{X: 0, Y: 0, W: 1920, H: 1080}, nil
}

// hyprDispatch uses the Lua dispatcher expressions required by Hyprland
// 0.56+ with a Lua config, and falls back to the older string syntax.
func (d *linuxDesktop) hyprDispatch(expression string, legacy ...string) error {
	out, err := Run(3*time.Second, "hyprctl", "dispatch", expression)
	if err == nil && !strings.Contains(strings.ToLower(string(out)), "error") {
		return nil
	}
	args := append([]string{"dispatch"}, legacy...)
	_, fallbackErr := Run(3*time.Second, "hyprctl", args...)
	if fallbackErr != nil && err != nil {
		return err
	}
	return fallbackErr
}

func (d *linuxDesktop) Activate(w *Window) error {
	if d.backgroundWindow() != nil {
		// Focusing would pull the user's workspace away, which is the whole
		// thing this path exists to avoid.
		return nil
	}
	if d.session == "wayland" {
		if d.hyprland && w.ID != "" && w.ID != "absolute" {
			return d.hyprDispatch(
				fmt.Sprintf(`hl.dsp.focus({ window = "address:%s" })`, w.ID),
				"focuswindow", "address:"+w.ID)
		}
		return nil // other compositors leave focus with the user
	}
	_, err := Run(3*time.Second, "xdotool", "windowactivate", w.ID)
	return err
}

func (d *linuxDesktop) MoveResize(w *Window, r Rect) error {
	if d.backgroundWindow() != nil {
		// Clicks and pixels are window-local, so the window only has to be
		// the size the templates expect; leave its position alone so the
		// user's layout is untouched.
		current, err := d.hyprGeometry(w.ID)
		if err != nil {
			return err
		}
		if current.W == r.W && current.H == r.H {
			return nil
		}
		return d.hyprResize(w.ID, current, r.W, r.H)
	}
	if d.session == "wayland" {
		if !d.hyprland || w.ID == "" || w.ID == "absolute" {
			return nil
		}
		address := "address:" + w.ID
		x, y, width, height := strconv.Itoa(r.X), strconv.Itoa(r.Y),
			strconv.Itoa(r.W), strconv.Itoa(r.H)
		// A tiled window ignores resizeactive. Hyprland's Lua float
		// dispatcher toggles even with action = "set", so read the state and
		// only float a window that is not floating. Resize before moving:
		// Hyprland keeps a floating window's centre when it is resized, which
		// is what made clicks land in the wrong place when the window was
		// moved first.
		floating, known := d.hyprWindowFloating(w.ID)
		if known && !floating {
			_ = d.hyprDispatch(
				fmt.Sprintf(`hl.dsp.window.float({ window = "%s" })`, address),
				"togglefloating", address)
		}
		if err := d.hyprDispatch(
			fmt.Sprintf(`hl.dsp.window.resize({ window = "%s", x = %s, y = %s, relative = false })`, address, width, height),
			"resizeactive", "exact", width, height); err != nil {
			return err
		}
		return d.hyprDispatch(
			fmt.Sprintf(`hl.dsp.window.move({ window = "%s", x = %s, y = %s, relative = false })`, address, x, y),
			"moveactive", "exact", x, y)
	}
	if _, err := Run(3*time.Second, "xdotool", "windowmove", w.ID,
		strconv.Itoa(r.X), strconv.Itoa(r.Y)); err != nil {
		return err
	}
	_, err := Run(3*time.Second, "xdotool", "windowsize", w.ID,
		strconv.Itoa(r.W), strconv.Itoa(r.H))
	return err
}

// hyprResize changes a window's size without moving it. A tiled window
// ignores resizeactive, so float it first when it is not floating yet.
func (d *linuxDesktop) hyprResize(address string, current Rect, width, height int) error {
	if !d.hyprland || address == "" || address == "absolute" {
		return nil
	}
	window := "address:" + address
	if floating, known := d.hyprWindowFloating(address); known && !floating {
		_ = d.hyprDispatch(
			fmt.Sprintf(`hl.dsp.window.float({ window = "%s" })`, window),
			"togglefloating", window)
	}
	if err := d.hyprDispatch(
		fmt.Sprintf(`hl.dsp.window.resize({ window = "%s", x = %d, y = %d, relative = false })`, window, width, height),
		"resizeactive", "exact", strconv.Itoa(width), strconv.Itoa(height)); err != nil {
		return err
	}
	// Resizing a floating window keeps its centre, which moves it. Put it back
	// where it was so a hidden window does not wander.
	return d.hyprDispatch(
		fmt.Sprintf(`hl.dsp.window.move({ window = "%s", x = %d, y = %d, relative = false })`, window, current.X, current.Y),
		"moveactive", "exact", strconv.Itoa(current.X), strconv.Itoa(current.Y))
}

// hyprWindowFloating reports whether a Hyprland window is floating. known is
// false when hyprctl could not say, so the caller does not toggle blindly.
func (d *linuxDesktop) hyprWindowFloating(address string) (floating, known bool) {
	out, err := Run(3*time.Second, "hyprctl", "-j", "clients")
	if err != nil {
		return false, false
	}
	var clients []struct {
		Address  string `json:"address"`
		Floating bool   `json:"floating"`
	}
	if err := json.Unmarshal(out, &clients); err != nil {
		return false, false
	}
	for _, c := range clients {
		if c.Address == address {
			return c.Floating, true
		}
	}
	return false, false
}

// Geometry reads the window rectangle back from the compositor. Hyprland
// re-centres a floating window while resizing, so the rectangle the macro
// asked for is not necessarily the one the window has.
func (d *linuxDesktop) Geometry(w *Window) (Rect, error) {
	if d.backgroundWindow() != nil {
		// Background capture is the window itself, so the rectangle always
		// starts at the origin and only the size is read back.
		rect, err := d.hyprGeometry(w.ID)
		if err != nil {
			return w.Rect, err
		}
		return Rect{X: 0, Y: 0, W: rect.W, H: rect.H}, nil
	}
	if d.session == "wayland" {
		if !d.hyprland || w.ID == "" || w.ID == "absolute" {
			return w.Rect, nil
		}
		rect, err := d.hyprGeometry(w.ID)
		if err != nil {
			return w.Rect, err
		}
		return rect, nil
	}
	if d.input == "xdotool" && w.ID != "" {
		out, err := Run(2*time.Second, "xdotool", "getwindowgeometry", "--shell", w.ID)
		if err != nil {
			return w.Rect, err
		}
		rect := w.Rect
		for _, line := range strings.Split(string(out), "\n") {
			key, value, ok := strings.Cut(line, "=")
			if !ok {
				continue
			}
			n, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil {
				continue
			}
			switch key {
			case "X":
				rect.X = n
			case "Y":
				rect.Y = n
			case "WIDTH":
				rect.W = n
			case "HEIGHT":
				rect.H = n
			}
		}
		if rect.Valid() {
			return rect, nil
		}
	}
	return w.Rect, nil
}

func (d *linuxDesktop) Capture() (image.Image, error) {
	if w := d.backgroundWindow(); w != nil {
		// `grim -T <stableId>` renders that toplevel on its own: no
		// workspace switch, no focus, and pixels of a window that is not on
		// screen at all. There is deliberately no fallback to a screen grab:
		// that image would be in screen coordinates and every crop after it
		// would be wrong.
		out, err := Run(10*time.Second, "grim", "-T", w.StableID, "-")
		if err != nil {
			return nil, fmt.Errorf("capture window %s: %w", w.StableID, err)
		}
		img, err := decode(out)
		if err != nil {
			return nil, fmt.Errorf("decode window capture: %w", err)
		}
		return img, nil
	}
	if d.screenshot == "" {
		return nil, fmt.Errorf("no screenshot tool found (install grim on Wayland or imagemagick/scrot on X11)")
	}
	stdoutArgs := map[string][]string{
		"grim":   {"-"},
		"import": {"-window", "root", "png:-"},
	}
	if args, ok := stdoutArgs[d.screenshot]; ok {
		out, err := Run(10*time.Second, d.screenshot, args...)
		if err == nil {
			if img, err := decode(out); err == nil {
				return img, nil
			}
		}
	}
	// The remaining programs write a file instead of stdout.
	dir, err := os.MkdirTemp("", "e7-refresh-shot-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "screen.png")
	fileArgs := map[string][]string{
		"scrot":            {"-o", path},
		"maim":             {"-o", path},
		"gnome-screenshot": {"-f", path},
		"spectacle":        {"-b", "-n", "-o", path},
		"xwd":              {"-out", filepath.Join(dir, "screen.xwd")},
	}
	args, ok := fileArgs[d.screenshot]
	if !ok {
		return nil, fmt.Errorf("unsupported screenshot tool %q", d.screenshot)
	}
	if _, err := Run(10*time.Second, d.screenshot, args...); err != nil {
		return nil, fmt.Errorf("%s: %w", d.screenshot, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	img, err := decode(data)
	if err != nil {
		return nil, fmt.Errorf("decode screenshot: %w", err)
	}
	return img, nil
}

func decode(data []byte) (image.Image, error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	return img, err
}

func (d *linuxDesktop) Click(x, y int) error {
	if d.backgroundWindow() != nil {
		in, err := d.xwaylandInput()
		if err != nil {
			return err
		}
		return in.Click(x, y)
	}
	xs, ys := strconv.Itoa(x), strconv.Itoa(y)
	if d.input == "ydotool" {
		// ydotool 1.0.4 aborts if mousemove and click share one invocation.
		if _, err := Run(3*time.Second, "ydotool", "mousemove", "--absolute", xs, ys); err != nil {
			return err
		}
		// ydotool uses a bitmask: 0x40 down + 0x80 up = left click.
		// Plain "1" is the right button in this version.
		_, err := Run(3*time.Second, "ydotool", "click", "0xC0")
		return err
	}
	_, err := Run(3*time.Second, "xdotool", "mousemove", "--sync", xs, ys, "click", "--clearmodifiers", "1")
	return err
}

func (d *linuxDesktop) DoubleClick(x, y int, interval time.Duration) error {
	if err := d.Click(x, y); err != nil {
		return err
	}
	time.Sleep(interval)
	return d.Click(x, y)
}

func (d *linuxDesktop) Drag(x1, y1, x2, y2 int) error {
	if d.backgroundWindow() != nil {
		in, err := d.xwaylandInput()
		if err != nil {
			return err
		}
		return in.Drag(x1, y1, x2, y2)
	}
	a, b, c, e := strconv.Itoa(x1), strconv.Itoa(y1), strconv.Itoa(x2), strconv.Itoa(y2)
	tool := d.input
	if tool == "ydotool" {
		for _, step := range [][]string{
			{"mousemove", "--absolute", a, b},
			{"click", "0x40"}, // left button down
			{"mousemove", "--absolute", c, e},
			{"click", "0x80"}, // left button up
		} {
			if _, err := Run(3*time.Second, tool, step...); err != nil {
				return err
			}
			time.Sleep(50 * time.Millisecond)
		}
		return nil
	}
	if _, err := Run(3*time.Second, tool, "mousemove", "--sync", a, b, "mousedown", "1"); err != nil {
		return err
	}
	time.Sleep(50 * time.Millisecond)
	if _, err := Run(3*time.Second, tool, "mousemove", "--sync", c, e); err != nil {
		return err
	}
	time.Sleep(50 * time.Millisecond)
	_, err := Run(3*time.Second, tool, "mouseup", "1")
	return err
}

func (d *linuxDesktop) Requirements() []Requirement {
	var reqs []Requirement
	// On Hyprland the XWayland path needs no input program at all. Only ask
	// for one when that path is not available, so a machine with grim alone
	// is a complete install.
	background := !d.foreground && d.session == "wayland" && d.hyprland && d.screenshot == "grim"
	if d.input == "" && !background {
		reqs = append(reqs, Requirement{
			Tool:    "xdotool",
			Why:     "mouse and window control (Wayland: ydotool)",
			Install: "arch: pacman -S xdotool; debian: apt install xdotool; wayland: pacman -S ydotool && systemctl --user enable --now ydotool",
		})
	}
	if d.screenshot == "" {
		name := "import"
		if d.session == "wayland" {
			name = "grim"
		}
		reqs = append(reqs, Requirement{
			Tool:    name,
			Why:     "screenshot capture (alternatives: scrot, maim, gnome-screenshot, spectacle)",
			Install: "arch: pacman -S imagemagick grim; debian: apt install imagemagick scrot grim",
		})
	}
	return reqs
}
