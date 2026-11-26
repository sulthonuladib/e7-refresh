// Package config owns the user settings of E7 Refresh.
//
// Settings are kept as JSON in the user's config directory and written
// atomically: a failed write leaves the previous settings intact.
package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// Item is one purchasable item the refresher looks for. Price is the gold
// cost of a single item, used for the history CSV only.
type Item struct {
	ID    string
	Name  string
	Price int
	File  string
}

// Items mirrors AppConfig.ALL_ITEMS of the upstream Python application.
// Cov and mys stay mandatory in the UI; friendship bookmarks are a slow but
// unambiguous way to check that template matching works.
var Items = []Item{
	{ID: "cov", Name: "Covenant bookmark", Price: 184000, File: "cov.png"},
	{ID: "mys", Name: "Mystic medal", Price: 280000, File: "mys.png"},
	{ID: "fb", Name: "Friendship bookmark", Price: 18000, File: "fb.png"},
}

// Config is the complete set of user settings, shared by the tray, the bar
// widget and every command line call. Durations are in seconds.
type Config struct {
	Mode            string          `json:"mode"`
	WindowTitle     string          `json:"windowTitle"`
	Items           map[string]bool `json:"items"`
	Budget          int             `json:"budget"`
	MouseSleep      float64         `json:"mouseSleep"`
	ScreenshotSleep float64         `json:"screenshotSleep"`
	TapSleep        float64         `json:"tapSleep"`
	AllowMove       bool            `json:"allowMove"`
	Hint            bool            `json:"hint"`
	RandomOffset    bool            `json:"randomOffset"`
	ADBAddress      string          `json:"adbAddress"`
	ADBPath         string          `json:"adbPath"`
	StopHotkey      string          `json:"stopHotkey"`
	AutoStart       bool            `json:"autoStart"`
}

// Default returns the settings the upstream application starts with.
func Default() Config {
	return Config{
		Mode:            "mouse",
		WindowTitle:     "Epic Seven",
		Items:           map[string]bool{"cov": true, "mys": true, "fb": false},
		Budget:          0,
		MouseSleep:      0.3,
		ScreenshotSleep: 0.3,
		TapSleep:        0.3,
		Hint:            true,
		StopHotkey:      "Escape",
	}
}

// Normalize clamps user input to the ranges the macros can work with and
// fills in anything missing from an older config file.
func (c *Config) Normalize() {
	def := Default()
	if c.Mode != "mouse" && c.Mode != "adb" {
		c.Mode = def.Mode
	}
	if c.Items == nil {
		c.Items = def.Items
	}
	c.Items["cov"] = true
	c.Items["mys"] = true
	if c.MouseSleep <= 0 {
		c.MouseSleep = def.MouseSleep
	}
	if c.ScreenshotSleep <= 0 {
		c.ScreenshotSleep = def.ScreenshotSleep
	}
	if c.TapSleep <= 0 {
		c.TapSleep = def.TapSleep
	}
	if c.MouseSleep > 10 {
		c.MouseSleep = 10
	}
	if c.ScreenshotSleep > 10 {
		c.ScreenshotSleep = 10
	}
	if c.TapSleep > 10 {
		c.TapSleep = 10
	}
	if c.Budget < 0 {
		c.Budget = 0
	}
	if c.StopHotkey == "" {
		c.StopHotkey = def.StopHotkey
	}
	if c.ADBAddress == "" {
		c.ADBAddress = "127.0.0.1:5555"
	}
}

// Dir is the directory that holds config.json.
func Dir() string {
	base, err := os.UserConfigDir()
	if err != nil {
		return filepath.Join(os.Getenv("HOME"), ".config", "e7-refresh")
	}
	return filepath.Join(base, "e7-refresh")
}

// DataDir holds the refresh history CSV files.
func DataDir() string {
	base, err := os.UserHomeDir()
	if err != nil {
		base = "."
	}
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, "e7-refresh")
	}
	return filepath.Join(base, ".local", "share", "e7-refresh")
}

// HistoryDir is where each finished run appends a CSV row.
func HistoryDir() string {
	return filepath.Join(DataDir(), "ShopRefreshHistory")
}

// RuntimeDir holds the daemon socket or address file. It is per-user and
// never world-writable: on Linux XDG_RUNTIME_DIR is used, on Windows the
// local application data directory, with a token in the address file.
func RuntimeDir() string {
	if runtime := os.Getenv("XDG_RUNTIME_DIR"); runtime != "" {
		return filepath.Join(runtime, "e7-refresh")
	}
	base, err := os.UserHomeDir()
	if err != nil {
		base = "."
	}
	return filepath.Join(base, ".local", "state", "e7-refresh")
}

// Load reads config.json, returning defaults when it does not exist yet.
func Load() (Config, error) {
	path := filepath.Join(Dir(), "config.json")
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		c := Default()
		return c, nil
	}
	if err != nil {
		return Config{}, err
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return Config{}, err
	}
	c.Normalize()
	return c, nil
}

// Save writes config.json beside its target first, then renames it into
// place, so a crash mid-write cannot leave a truncated settings file.
func Save(c Config) error {
	c.Normalize()
	dir := Dir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(dir, ".config-*.json")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, filepath.Join(dir, "config.json"))
}
