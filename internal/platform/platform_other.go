//go:build !linux && !windows

package platform

import (
	"fmt"
	"image"
	"time"
)

type otherDesktop struct{}

// New returns a desktop that reports the platform as unsupported. MyGo runs
// on macOS too, but the refresh macros are shipped for Linux and Windows.
func New() Desktop { return &otherDesktop{} }

func (d *otherDesktop) Kind() string { return "unsupported" }

func (d *otherDesktop) FindWindow(string) (*Window, error) {
	return nil, fmt.Errorf("mouse refresh is not supported on this platform")
}

func (d *otherDesktop) Activate(*Window) error { return fmt.Errorf("unsupported") }

func (d *otherDesktop) MoveResize(*Window, Rect) error { return fmt.Errorf("unsupported") }

func (d *otherDesktop) Capture() (image.Image, error) { return nil, fmt.Errorf("unsupported") }

func (d *otherDesktop) Click(int, int) error { return fmt.Errorf("unsupported") }

func (d *otherDesktop) DoubleClick(int, int, time.Duration) error { return fmt.Errorf("unsupported") }

func (d *otherDesktop) Drag(int, int, int, int) error { return fmt.Errorf("unsupported") }

func (d *otherDesktop) KeyPressed(string) bool { return false }

func (d *otherDesktop) Requirements() []Requirement { return nil }
