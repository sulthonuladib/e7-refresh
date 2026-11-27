//go:build linux

package platform

import (
	"image/png"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestBackgroundWindowClick is the manual proof that a window can be captured
// and clicked while it stays hidden and unfocused. It only runs when
// E7_REFRESH_TEST_WINDOW names the window and E7_REFRESH_TEST_CLICK gives a
// window-local point, so `go test ./...` never touches a running game.
//
//	E7_REFRESH_TEST_WINDOW="Epic Seven" \
//	E7_REFRESH_TEST_CLICK=282,33 go test ./internal/platform -run Background -v
func TestBackgroundWindowClick(t *testing.T) {
	title := os.Getenv("E7_REFRESH_TEST_WINDOW")
	point := os.Getenv("E7_REFRESH_TEST_CLICK")
	if title == "" || point == "" {
		t.Skip("set E7_REFRESH_TEST_WINDOW and E7_REFRESH_TEST_CLICK to run this")
	}
	parts := strings.Split(point, ",")
	if len(parts) != 2 {
		t.Fatalf("E7_REFRESH_TEST_CLICK must be x,y")
	}
	x, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		t.Fatal(err)
	}
	y, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil {
		t.Fatal(err)
	}

	desktop := New()
	if closer, ok := desktop.(interface{ Close() error }); ok {
		defer closer.Close()
	}
	window, err := desktop.FindWindow(title)
	if err != nil {
		t.Fatal(err)
	}
	if !desktop.Background() {
		t.Fatalf("window %q is not background-capable (needs XWayland on Hyprland)", title)
	}
	frame, err := desktop.Capture()
	if err != nil {
		t.Fatal(err)
	}
	geometry, err := desktop.Geometry(window)
	if err != nil {
		t.Fatal(err)
	}
	bounds := frame.Bounds()
	if bounds.Dx() != geometry.W || bounds.Dy() != geometry.H {
		t.Fatalf("capture %dx%d does not match geometry %dx%d", bounds.Dx(), bounds.Dy(), geometry.W, geometry.H)
	}
	t.Logf("captured %q at %dx%d without focusing it", title, bounds.Dx(), bounds.Dy())
	if path := os.Getenv("E7_REFRESH_TEST_SAVE"); path != "" {
		file, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(file, frame); err != nil {
			t.Fatal(err)
		}
		file.Close()
	}
	if os.Getenv("E7_REFRESH_TEST_NO_CLICK") != "" {
		return
	}
	if drag := os.Getenv("E7_REFRESH_TEST_DRAG"); drag != "" {
		fields := strings.Split(drag, ",")
		if len(fields) != 4 {
			t.Fatalf("E7_REFRESH_TEST_DRAG must be x1,y1,x2,y2")
		}
		var n [4]int
		for i, field := range fields {
			value, err := strconv.Atoi(strings.TrimSpace(field))
			if err != nil {
				t.Fatal(err)
			}
			n[i] = value
		}
		if err := desktop.Drag(n[0], n[1], n[2], n[3]); err != nil {
			t.Fatal(err)
		}
		t.Logf("dragged %d,%d -> %d,%d and captured again", n[0], n[1], n[2], n[3])
		return
	}
	before := desktop
	if err := before.Click(x, y); err != nil {
		t.Fatal(err)
	}
	time.Sleep(700 * time.Millisecond)
	after, err := desktop.Capture()
	if err != nil {
		t.Fatal(err)
	}
	if bounds := after.Bounds(); bounds.Dx() != frame.Bounds().Dx() || bounds.Dy() != frame.Bounds().Dy() {
		t.Fatalf("capture changed size after click")
	}
	t.Logf("clicked %d,%d and captured again", x, y)
}
