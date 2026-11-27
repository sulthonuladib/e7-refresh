package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/sulthonuladib/e7-refresh/internal/desktop"
)

// installBar installs the QML widget into the user's Omarchy shell plugins
// and adds it to the bar. It never touches /usr/share/omarchy: everything it
// changes lives under ~/.config and is backed up first.
func installBar() {
	if err := installBarE(); err != nil {
		fail(err)
	}
	fmt.Println("added e7-refresh to the bar; Omarchy reloads the plugin and shell.json automatically")
}

func installBarE() error {
	if err := ensureBarPlugin(); err != nil {
		return err
	}
	fmt.Printf("installed bar widget in %s\n", barPluginDir())
	return nil
}

// ensureBarPlugin copies the current QML into the user's plugin directory and
// makes sure the widget is in the bar. It runs on every Omarchy launch so a
// new build's panel replaces the old one; it never touches /usr/share.
func ensureBarPlugin() error {
	info := desktop.Detect()
	if info.Desktop != "omarchy" {
		return fmt.Errorf("Omarchy is not detected (desktop=%s); other desktops use the AppIndicator tray", info.Desktop)
	}
	candidates := []string{"omarchy", "resources/omarchy"}
	if exe, err := os.Executable(); err == nil {
		base := filepath.Dir(exe)
		candidates = append(candidates, filepath.Join(base, "omarchy"), filepath.Join(base, "resources", "omarchy"))
	}
	source := ""
	for _, candidate := range candidates {
		if _, err := os.Stat(filepath.Join(candidate, "manifest.json")); err == nil {
			source = candidate
			break
		}
	}
	if _, err := os.Stat(filepath.Join(source, "manifest.json")); err != nil {
		return fmt.Errorf("bar plugin files not found in %q", source)
	}
	if err := copyTree(source, barPluginDir()); err != nil {
		return err
	}
	return addBarWidget(filepath.Join(os.Getenv("HOME"), ".config", "omarchy", "shell.json"))
}

func barPluginDir() string {
	return filepath.Join(os.Getenv("HOME"), ".config", "omarchy", "plugins", "e7-refresh")
}

// barWidgetInstalled reports whether the Omarchy plugin is both on disk and
// in the bar layout, so the menu can stop offering to install it again.
func barWidgetInstalled() bool {
	home := os.Getenv("HOME")
	if home == "" {
		return false
	}
	if _, err := os.Stat(filepath.Join(barPluginDir(), "manifest.json")); err != nil {
		return false
	}
	data, err := os.ReadFile(filepath.Join(home, ".config", "omarchy", "shell.json"))
	if err != nil {
		return false
	}
	var shell map[string]any
	if err := json.Unmarshal(data, &shell); err != nil {
		return false
	}
	bar, _ := shell["bar"].(map[string]any)
	layout, _ := bar["layout"].(map[string]any)
	right, _ := layout["right"].([]any)
	for _, entry := range right {
		if item, ok := entry.(map[string]any); ok && item["id"] == "e7-refresh" {
			return true
		}
	}
	return false
}

func serviceUnitPath() string {
	return filepath.Join(os.Getenv("HOME"), ".config", "systemd", "user", "e7-refresh.service")
}

func serviceUnit(executable string) string {
	return fmt.Sprintf(`[Unit]
Description=E7 Secret Shop Refresh control service
After=graphical-session.target

[Service]
Type=simple
ExecStart=%s service
Restart=on-failure
RestartSec=3

[Install]
WantedBy=default.target
`, executable)
}

func runSystemctl(args ...string) error {
	cmd := exec.Command("systemctl", append([]string{"--user"}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl %s: %s: %w", strings.Join(args, " "), strings.TrimSpace(string(out)), err)
	}
	return nil
}

func installService() {
	if err := installServiceE(); err != nil {
		fail(err)
	}
	fmt.Printf("installed and enabled %s\n", serviceUnitPath())
}

func installServiceE() error {
	path := serviceUnitPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(serviceUnit(executable)), 0o644); err != nil {
		return err
	}
	if err := runSystemctl("daemon-reload"); err != nil {
		return err
	}
	return runSystemctl("enable", "--now", "e7-refresh.service")
}

func removeService() {
	if err := removeServiceE(); err != nil {
		fail(err)
	}
	fmt.Println("background service removed")
}

func removeServiceE() error {
	if err := runSystemctl("disable", "--now", "e7-refresh.service"); err != nil {
		return err
	}
	if err := os.Remove(serviceUnitPath()); err != nil && !os.IsNotExist(err) {
		return err
	}
	return runSystemctl("daemon-reload")
}

func copyTree(source, target string) error {
	return filepath.Walk(source, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		dest := filepath.Join(target, rel)
		if info.IsDir() {
			return os.MkdirAll(dest, 0o755)
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		defer out.Close()
		_, err = io.Copy(out, in)
		return err
	})
}

func addBarWidget(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var shell map[string]any
	if err := json.Unmarshal(data, &shell); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	bar, ok := shell["bar"].(map[string]any)
	if !ok {
		return fmt.Errorf("%s has no bar section", path)
	}
	layout, ok := bar["layout"].(map[string]any)
	if !ok {
		return fmt.Errorf("%s has no bar.layout section", path)
	}
	right, ok := layout["right"].([]any)
	if !ok {
		return fmt.Errorf("%s has no bar.layout.right section", path)
	}
	for _, entry := range right {
		if item, ok := entry.(map[string]any); ok && item["id"] == "e7-refresh" {
			return nil // already installed, do not add it twice
		}
	}
	backup := fmt.Sprintf("%s.bak.%d", path, time.Now().Unix())
	if err := os.WriteFile(backup, data, 0o600); err != nil {
		return err
	}
	layout["right"] = append(right, map[string]any{"id": "e7-refresh"})
	updated, err := json.MarshalIndent(shell, "", "  ")
	if err != nil {
		return err
	}
	updated = append(updated, '\n')
	tmp := path + ".new"
	if err := os.WriteFile(tmp, updated, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

const skillMarkdown = `---
name: e7-refresh
description: Control the E7 Secret Shop Refresh daemon from the command line. Use it to check status, start or stop refreshes, change settings and locate refresh history.
---

# E7 Refresh

The app runs a local control service shared by the tray, the Omarchy bar and
the CLI. Commands exit 0 on success and print errors on stderr.

- ` + "`e7-refresh status --json`" + ` — current run as JSON: running, mode, refreshCount, counts, spent, gold, lastError.
- ` + "`e7-refresh probe --json`" + ` — score every bookmark template in the current window without clicking; saves an annotated image.
- ` + "`e7-refresh settings`" + ` — open the settings window (on Omarchy the bar panel is the primary UI).
- ` + "`e7-refresh start`" + ` — start a run with the saved settings. Add ` + "`--config '{\"mode\":\"adb\"}'`" + ` to override them for this run.
- ` + "`e7-refresh stop`" + ` — ask the current run to stop after its current action.
- ` + "`e7-refresh config --json`" + ` — read settings; ` + "`--set '<json>'`" + ` writes them atomically.
- ` + "`e7-refresh detect --json`" + ` — desktop, session type (x11/wayland/windows) and available tools.
- ` + "`e7-refresh history`" + ` — directory of the CSV history files.

Never change a setting without telling the user what changed. Do not start a
run when ` + "`status`" + ` already reports running. The GUI is optional: the
` + "`service`" + ` subcommand runs the same engine with no display.
`

func installSkill() {
	dir := filepath.Join(".agents", "skills", "e7-refresh")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fail(err)
	}
	path := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(path, []byte(skillMarkdown), 0o644); err != nil {
		fail(err)
	}
	fmt.Printf("installed %s\n", path)
}
