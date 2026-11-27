// Package desktop identifies the desktop the app is running on so the tray,
// bar integration and README can pick the right mechanism. It only reads
// environment variables and files; it never changes the user's system.
package desktop

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/sulthonuladib/e7-refresh/internal/platform"
)

// Info is the detected desktop, exposed over IPC to the bar widgets.
type Info struct {
	OS              string `json:"os"`
	Distro          string `json:"distro"`
	Session         string `json:"session"`
	Desktop         string `json:"desktop"`
	Bar             string `json:"bar"`
	HasAppIndicator bool   `json:"hasAppIndicator"`
	ScreenshotTool  string `json:"screenshotTool"`
	InputTool       string `json:"inputTool"`
	// BackgroundControl says whether a refresh can capture and click the game
	// while it stays hidden and unfocused; BackgroundMethod explains how.
	BackgroundControl bool   `json:"backgroundControl"`
	BackgroundMethod  string `json:"backgroundMethod"`
}

// Detect returns the current desktop environment.
func Detect() Info {
	info := Info{
		OS:      runtime.GOOS,
		Distro:  distro(),
		Session: platform.New().Kind(),
	}
	info.BackgroundControl, info.BackgroundMethod = platform.BackgroundCapability()
	name := strings.ToLower(os.Getenv("XDG_CURRENT_DESKTOP"))
	if info.OS == "windows" {
		info.Desktop = "windows"
		info.Bar = "windows-tray"
		return info
	}
	if _, err := os.Stat(filepath.Join(os.Getenv("HOME"), ".local", "state", "omarchy")); err == nil ||
		strings.Contains(name, "omarchy") {
		info.Desktop = "omarchy"
	} else {
		switch {
		case strings.Contains(name, "gnome"):
			info.Desktop = "gnome"
		case strings.Contains(name, "kde") || strings.Contains(name, "plasma"):
			info.Desktop = "plasma"
		case os.Getenv("HYPRLAND_INSTANCE_SIGNATURE") != "":
			info.Desktop = "hyprland"
		case strings.Contains(name, "sway"):
			info.Desktop = "sway"
		case strings.Contains(name, "xfce"):
			info.Desktop = "xfce"
		case name != "":
			info.Desktop = name
		default:
			info.Desktop = "generic"
		}
	}
	info.HasAppIndicator = hasAppIndicator()
	switch {
	case info.Desktop == "omarchy":
		info.Bar = "omarchy-qml"
	case info.HasAppIndicator:
		info.Bar = "appindicator"
	default:
		info.Bar = "generic"
	}
	info.ScreenshotTool = firstOnPath("grim", "import", "scrot", "maim", "gnome-screenshot", "spectacle")
	info.InputTool = firstOnPath("xdotool", "ydotool")
	return info
}

func distro() string {
	f, err := os.Open("/etc/os-release")
	if err != nil {
		return ""
	}
	defer f.Close()
	values := map[string]string{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		key, value, ok := strings.Cut(scanner.Text(), "=")
		if !ok {
			continue
		}
		values[key] = strings.Trim(strings.TrimSpace(value), `"`)
	}
	if id := values["ID"]; id != "" {
		return id
	}
	return values["ID_LIKE"]
}

func hasAppIndicator() bool {
	for _, path := range []string{
		"/usr/lib/libayatana-appindicator3.so.1",
		"/usr/lib64/libayatana-appindicator3.so.1",
		"/usr/lib/x86_64-linux-gnu/libayatana-appindicator3.so.1",
		"/usr/lib/aarch64-linux-gnu/libayatana-appindicator3.so.1",
	} {
		if _, err := os.Stat(path); err == nil {
			return true
		}
	}
	out, err := exec.Command("ldconfig", "-p").Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), "libayatana-appindicator3.so")
}

func firstOnPath(names ...string) string {
	for _, name := range names {
		if _, err := exec.LookPath(name); err == nil {
			return name
		}
	}
	return ""
}
