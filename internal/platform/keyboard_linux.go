//go:build linux

package platform

import (
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"sync"
	"syscall"
)

// inputEvent mirrors the kernel's struct input_event. Reading /dev/input
// works on both X11 and Wayland, unlike compositor-specific key grabs, so a
// stop key can be used anywhere without installing a desktop shortcut.
type inputEvent struct {
	Time syscall.Timeval
	Type uint16
	Code uint16
	Value int32
}

var evdevCodes = map[string]uint16{
	"escape": 1, "esc": 1,
	"q": 16, "s": 31, "space": 57, "enter": 28, "return": 28,
}

// WatchKey calls fn once when key is pressed anywhere in the session.
func WatchKey(ctx context.Context, key string, fn func()) {
	code, ok := evdevCodes[lower(key)]
	if !ok {
		return
	}
	files, err := filepath.Glob("/dev/input/event*")
	if err != nil || len(files) == 0 {
		return
	}
	var once sync.Once
	trigger := func() { once.Do(fn) }
	var wg sync.WaitGroup
	opened := make([]*os.File, 0, len(files))
	for _, path := range files {
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		opened = append(opened, f)
		wg.Add(1)
		go func(f *os.File) {
			defer wg.Done()
			defer f.Close()
			for {
				var event inputEvent
				if err := binary.Read(f, binary.LittleEndian, &event); err != nil {
					return
				}
				if event.Type == 1 && event.Code == code && event.Value == 1 {
					trigger()
					return
				}
			}
		}(f)
	}
	go func() {
		<-ctx.Done()
		for _, f := range opened {
			f.Close() // unblocks the readers
		}
		wg.Wait()
	}()
}

func lower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}
