package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/sulthonuladib/e7-refresh/internal/platform"
)

// floatSettingsWindow floats and centres the app's own window on Hyprland.
// A tiling compositor would otherwise stretch the two-column settings form
// across a tile, and the window would open at a size it was not laid out for.
func floatSettingsWindow() {
	if !hyprlandRunning() {
		return
	}
	// The window appears in hyprctl's client list a frame or two after Show,
	// and a fresh toplevel can be tiled again on the frame it maps, so impose
	// the floating geometry and verify it a few times.
	for attempt := 0; attempt < 5; attempt++ {
		clients, err := hyprClients()
		if err != nil {
			time.Sleep(150 * time.Millisecond)
			continue
		}
		var target *hyprClient
		for i := range clients {
			if clients[i].Title == "E7 Secret Shop Refresh" {
				target = &clients[i]
				break
			}
		}
		if target == nil {
			time.Sleep(150 * time.Millisecond)
			continue
		}
		monitors, err := hyprMonitors()
		if err != nil {
			time.Sleep(150 * time.Millisecond)
			continue
		}
		var monitor *hyprMonitor
		for i := range monitors {
			if monitors[i].ID == target.Monitor {
				monitor = &monitors[i]
				break
			}
		}
		if monitor == nil {
			time.Sleep(150 * time.Millisecond)
			continue
		}
		left, top, right, bottom := 0, 0, 0, 0
		if len(monitor.Reserved) >= 4 {
			left, top, right, bottom = monitor.Reserved[0], monitor.Reserved[1], monitor.Reserved[2], monitor.Reserved[3]
		}
		usableW := monitor.Width - left - right
		usableH := monitor.Height - top - bottom
		if usableW <= 0 || usableH <= 0 {
			return
		}
		width := min(760, usableW-40)
		height := min(720, usableH-40)
		x := left + (usableW-width)/2
		y := top + (usableH-height)/2
		address := "address:" + target.Address
		// Hyprland's Lua float dispatcher toggles even with action = "set",
		// so only call it when the window is actually tiled.
		if !target.Floating {
			_ = hyprDispatch(
				fmt.Sprintf(`hl.dsp.window.float({ window = "%s" })`, address),
				"togglefloating", address)
		}
		if err := hyprDispatch(
			fmt.Sprintf(`hl.dsp.window.resize({ window = "%s", x = %d, y = %d, relative = false })`, address, width, height),
			"resizeactive", "exact", strconv.Itoa(width), strconv.Itoa(height)); err != nil {
			return
		}
		_ = hyprDispatch(
			fmt.Sprintf(`hl.dsp.window.move({ window = "%s", x = %d, y = %d, relative = false })`, address, x, y),
			"moveactive", "exact", strconv.Itoa(x), strconv.Itoa(y))
		time.Sleep(150 * time.Millisecond)
		clients, err = hyprClients()
		if err != nil {
			continue
		}
		for i := range clients {
			if clients[i].Address == target.Address {
				c := clients[i]
				if c.Floating && len(c.At) >= 2 && len(c.Size) >= 2 &&
					c.At[0] == x && c.At[1] == y && c.Size[0] == width && c.Size[1] == height {
					return
				}
				break
			}
		}
	}
}

func hyprlandRunning() bool {
	if os.Getenv("HYPRLAND_INSTANCE_SIGNATURE") != "" {
		return true
	}
	runtimeDir := os.Getenv("XDG_RUNTIME_DIR")
	if runtimeDir == "" {
		runtimeDir = fmt.Sprintf("/run/user/%d", os.Getuid())
	}
	entries, err := os.ReadDir(filepath.Join(runtimeDir, "hypr"))
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if entry.IsDir() {
			_ = os.Setenv("HYPRLAND_INSTANCE_SIGNATURE", entry.Name())
			return true
		}
	}
	return false
}

type hyprClient struct {
	Title    string `json:"title"`
	Class    string `json:"class"`
	Address  string `json:"address"`
	Monitor  int    `json:"monitor"`
	At       []int  `json:"at"`
	Size     []int  `json:"size"`
	Floating bool   `json:"floating"`
}

func hyprClients() ([]hyprClient, error) {
	out, err := platform.Run(3*time.Second, "hyprctl", "-j", "clients")
	if err != nil {
		return nil, err
	}
	var clients []hyprClient
	if err := json.Unmarshal(out, &clients); err != nil {
		return nil, err
	}
	return clients, nil
}

type hyprMonitor struct {
	ID       int   `json:"id"`
	Width    int   `json:"width"`
	Height   int   `json:"height"`
	Reserved []int `json:"reserved"`
}

func hyprMonitors() ([]hyprMonitor, error) {
	out, err := platform.Run(3*time.Second, "hyprctl", "-j", "monitors")
	if err != nil {
		return nil, err
	}
	var monitors []hyprMonitor
	if err := json.Unmarshal(out, &monitors); err != nil {
		return nil, err
	}
	return monitors, nil
}

// hyprDispatch uses the Lua dispatcher syntax of Hyprland 0.56+ and falls
// back to the older dispatcher names when the compositor does not run Lua.
func hyprDispatch(expression string, legacy ...string) error {
	out, err := platform.Run(3*time.Second, "hyprctl", "dispatch", expression)
	if err == nil && !strings.Contains(strings.ToLower(string(out)), "error") {
		return nil
	}
	args := append([]string{"dispatch"}, legacy...)
	_, fallbackErr := platform.Run(3*time.Second, "hyprctl", args...)
	if fallbackErr != nil && err != nil {
		return err
	}
	return fallbackErr
}
