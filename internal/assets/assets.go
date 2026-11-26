// Package assets locates the PNG templates shipped next to the executable.
//
// MyGo copies resources/ beside the app; in a source checkout the same
// directory is at the repository root. E7_REFRESH_ASSETS overrides both so
// tests and alternate installs can point somewhere else.
package assets

import (
	"fmt"
	"os"
	"path/filepath"
)

// Dir returns the directory that holds assets/ and adb-assets/.
func Dir() string {
	if dir := os.Getenv("E7_REFRESH_ASSETS"); dir != "" {
		return dir
	}
	if exe, err := os.Executable(); err == nil {
		// mygo build copies the contents of resources/ next to the binary;
		// a plain `go build` with bin/build keeps them in resources/.
		base := filepath.Dir(exe)
		for _, dir := range []string{base, filepath.Join(base, "resources")} {
			if _, err := os.Stat(filepath.Join(dir, "assets")); err == nil {
				return dir
			}
		}
	}
	if cwd, err := os.Getwd(); err == nil {
		for dir := cwd; ; dir = filepath.Dir(dir) {
			if _, err := os.Stat(filepath.Join(dir, "resources", "assets")); err == nil {
				return filepath.Join(dir, "resources")
			}
			if parent := filepath.Dir(dir); parent == dir {
				break
			}
		}
	}
	return "resources"
}

// Pattern returns the absolute path of a template image.
// kind is "assets" (mouse mode) or "adb-assets" (ADB mode).
func Pattern(kind, name string) (string, error) {
	path := filepath.Join(Dir(), kind, name)
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("missing template %s: %w", name, err)
	}
	return path, nil
}
