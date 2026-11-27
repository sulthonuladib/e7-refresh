//go:build windows

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
// and clicked while the user keeps working. It only runs when the environment
// names the window, so `go test ./...` never touches a running game.
//
//	E7_REFRESH_TEST_WINDOW="Epic Seven" ^
//	E7_REFRESH_TEST_SAVE=C:\tmp\e7.png ^
//	E7_REFRESH_TEST_CLICK=282,33 ^
//	go test ./internal/platform -run Background -v
//
// 282,33 is the secret shop's help button: it opens a help page that closes
// again with a right click, so it makes no purchase and spends nothing.
func TestBackgroundWindowClick(t *testing.T) {
	title := os.Getenv("E7_REFRESH_TEST_WINDOW")
	if title == "" {
		t.Skip("set E7_REFRESH_TEST_WINDOW to run this")
	}
	desktop := New()
	window, err := desktop.FindWindow(title)
	if err != nil {
		t.Fatal(err)
	}
	if !desktop.Background() {
		t.Fatalf("window %q is not background-capable: it did not render with PrintWindow, "+
			"or it is minimized. Restore the game, not minimized, and try again", title)
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
	t.Logf("captured %q at %dx%d without showing or focusing it", title, bounds.Dx(), bounds.Dy())
	if path := os.Getenv("E7_REFRESH_TEST_SAVE"); path != "" {
		file, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(file, frame); err != nil {
			file.Close()
			t.Fatal(err)
		}
		file.Close()
		t.Logf("saved %s", path)
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
		t.Logf("dragged %d,%d -> %d,%d", n[0], n[1], n[2], n[3])
	}
	point := os.Getenv("E7_REFRESH_TEST_CLICK")
	if point == "" {
		return
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
	if err := desktop.Click(x, y); err != nil {
		t.Fatal(err)
	}
	time.Sleep(700 * time.Millisecond)
	if _, err := desktop.Capture(); err != nil {
		t.Fatalf("capture after click: %v", err)
	}
	t.Logf("clicked %d,%d. If the game did not react, it ignores posted messages and "+
		"needs E7_REFRESH_FOREGROUND=1 (the window is shown and the pointer moves)", x, y)
}
