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
	"time"
)

type linuxDesktop struct {
	session   string // "x11" or "wayland"
	hyprland  bool
	screenshot string // first available screenshot program
	input      string // first available input program
}

// New returns the desktop implementation matching the current session.
func New() Desktop {
	session := strings.ToLower(os.Getenv("XDG_SESSION_TYPE"))
	if session == "" {
		switch {
		case os.Getenv("WAYLAND_DISPLAY") != "":
			session = "wayland"
		case os.Getenv("DISPLAY") != "":
			session = "x11"
		}
	}
	if session != "wayland" {
		session = "x11"
	}
	d := &linuxDesktop{
		session:  session,
		hyprland: os.Getenv("HYPRLAND_INSTANCE_SIGNATURE") != "",
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

func (d *linuxDesktop) Kind() string { return d.session }

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
		Title   string `json:"title"`
		Address string `json:"address"`
		At      []int  `json:"at"`
		Size    []int  `json:"size"`
	}
	if err := json.Unmarshal(out, &clients); err != nil {
		return nil, fmt.Errorf("hyprctl clients JSON: %w", err)
	}
	for _, c := range clients {
		if c.Title != title || len(c.At) < 2 || len(c.Size) < 2 {
			continue
		}
		return &Window{
			Title: title,
			ID:    c.Address,
			Rect:  Rect{X: c.At[0], Y: c.At[1], W: c.Size[0], H: c.Size[1]},
		}, nil
	}
	return nil, fmt.Errorf("no visible window titled %q", title)
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

func (d *linuxDesktop) Activate(w *Window) error {
	if d.session == "wayland" {
		if d.hyprland && w.ID != "" && w.ID != "absolute" {
			_, err := Run(3*time.Second, "hyprctl", "dispatch", "focuswindow", "address:"+w.ID)
			return err
		}
		return nil // other compositors leave focus with the user
	}
	_, err := Run(3*time.Second, "xdotool", "windowactivate", w.ID)
	return err
}

func (d *linuxDesktop) MoveResize(w *Window, r Rect) error {
	if d.session == "wayland" {
		if !d.hyprland || w.ID == "" || w.ID == "absolute" {
			return nil
		}
		if _, err := Run(3*time.Second, "hyprctl", "dispatch", "moveactive", "exact",
			strconv.Itoa(r.X), strconv.Itoa(r.Y)); err != nil {
			return err
		}
		_, err := Run(3*time.Second, "hyprctl", "dispatch", "resizeactive", "exact",
			strconv.Itoa(r.W), strconv.Itoa(r.H))
		return err
	}
	if _, err := Run(3*time.Second, "xdotool", "windowmove", w.ID,
		strconv.Itoa(r.X), strconv.Itoa(r.Y)); err != nil {
		return err
	}
	_, err := Run(3*time.Second, "xdotool", "windowsize", w.ID,
		strconv.Itoa(r.W), strconv.Itoa(r.H))
	return err
}

func (d *linuxDesktop) Capture() (image.Image, error) {
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
		"scrot":           {"-o", path},
		"maim":            {"-o", path},
		"gnome-screenshot": {"-f", path},
		"spectacle":       {"-b", "-n", "-o", path},
		"xwd":             {"-out", filepath.Join(dir, "screen.xwd")},
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
	xs, ys := strconv.Itoa(x), strconv.Itoa(y)
	if d.input == "ydotool" {
		_, err := Run(3*time.Second, "ydotool", "mousemove", "--absolute", xs, ys, "click", "1")
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
	a, b, c, e := strconv.Itoa(x1), strconv.Itoa(y1), strconv.Itoa(x2), strconv.Itoa(y2)
	tool := d.input
	if tool == "ydotool" {
		_, err := Run(3*time.Second, tool, "mousemove", "--absolute", a, b, "mousedown", "1")
		if err != nil {
			return err
		}
		time.Sleep(50 * time.Millisecond)
		_, err = Run(3*time.Second, tool, "mousemove", "--absolute", c, e)
		time.Sleep(50 * time.Millisecond)
		_, err2 := Run(3*time.Second, tool, "mouseup", "1")
		if err != nil {
			return err
		}
		return err2
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

func (d *linuxDesktop) KeyPressed(key string) bool { return false }

func (d *linuxDesktop) Requirements() []Requirement {
	var reqs []Requirement
	if d.input == "" {
		reqs = append(reqs, Requirement{
			Tool:    "xdotool",
			Why:     "mouse and window control (Wayland: ydotool)",
			Install: "arch: pacman -S xdotool; debian: apt install xdotool; wayland: install ydotool",
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
