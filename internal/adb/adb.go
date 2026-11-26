// Package adb drives an Android emulator through the adb command line tool.
// Screenshots come from `exec-out screencap -p`, taps and swipes from
// `shell input`, exactly as in the upstream Python application.
package adb

import (
	"bytes"
	"context"
	"fmt"
	"image"
	_ "image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ADB is one selected device. Device may be empty for the only connected
// device, or an address such as 127.0.0.1:5555.
type ADB struct {
	Path   string
	Device string
}

// Resolve finds the adb executable: an explicit path first, then the one
// shipped beside the app, then one on PATH.
func Resolve(explicit string) (string, error) {
	if explicit != "" {
		if _, err := os.Stat(explicit); err == nil {
			return explicit, nil
		}
		return "", fmt.Errorf("adb not found at %q", explicit)
	}
	if exe, err := os.Executable(); err == nil {
		for _, candidate := range []string{
			filepath.Join(filepath.Dir(exe), "resources", "platform-tools", "adb"),
			filepath.Join(filepath.Dir(exe), "platform-tools", "adb"),
			filepath.Join(filepath.Dir(exe), "adb.exe"),
		} {
			if _, err := os.Stat(candidate); err == nil {
				return candidate, nil
			}
		}
	}
	path, err := exec.LookPath("adb")
	if err != nil {
		return "", fmt.Errorf("adb not found; install Android platform-tools or set adbPath in settings")
	}
	return path, nil
}

func (a *ADB) args() []string {
	if a.Device == "" {
		return nil
	}
	return []string{"-s", a.Device}
}

func (a *ADB) run(timeout time.Duration, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, a.Path, append(a.args(), args...)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = err.Error()
		}
		return nil, fmt.Errorf("adb %s: %s", strings.Join(args, " "), detail)
	}
	return stdout.Bytes(), nil
}

// Devices lists connected device identifiers.
func Devices(path string) ([]string, error) {
	a := &ADB{Path: path}
	out, err := a.run(10*time.Second, "devices")
	if err != nil {
		return nil, err
	}
	var devices []string
	for _, line := range strings.Split(string(out), "\n")[1:] {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[1] == "device" {
			devices = append(devices, fields[0])
		}
	}
	return devices, nil
}

// Connect attaches a device over TCP, usually an emulator's ADB port.
func (a *ADB) Connect(address string) error {
	if address == "" {
		return nil
	}
	out, err := a.run(15*time.Second, "connect", address)
	if err != nil {
		return err
	}
	if !strings.Contains(string(out), "connected") {
		return fmt.Errorf("adb connect %s: %s", address, strings.TrimSpace(string(out)))
	}
	a.Device = address
	return nil
}

// Screenshot returns the device frame as an image.
func (a *ADB) Screenshot() (image.Image, error) {
	out, err := a.run(15*time.Second, "exec-out", "screencap", "-p")
	if err != nil {
		return nil, err
	}
	img, _, err := image.Decode(bytes.NewReader(out))
	if err != nil {
		return nil, fmt.Errorf("decode screencap: %w", err)
	}
	return img, nil
}

// Tap sends one tap at physical device coordinates.
func (a *ADB) Tap(x, y float64) error {
	_, err := a.run(5*time.Second, "shell", "input", "tap",
		fmt.Sprintf("%.0f", x), fmt.Sprintf("%.0f", y))
	return err
}

// Swipe sends a drag gesture between two physical device coordinates.
func (a *ADB) Swipe(x1, y1, x2, y2 float64) error {
	_, err := a.run(5*time.Second, "shell", "input", "swipe",
		fmt.Sprintf("%.0f", x1), fmt.Sprintf("%.0f", y1),
		fmt.Sprintf("%.0f", x2), fmt.Sprintf("%.0f", y2))
	return err
}

// CheckDimension returns the device resolution and whether it is the 1920x1080
// size the macro coordinates assume.
func (a *ADB) CheckDimension() (int, int, error) {
	img, err := a.Screenshot()
	if err != nil {
		return 0, 0, err
	}
	b := img.Bounds()
	return b.Dx(), b.Dy(), nil
}
