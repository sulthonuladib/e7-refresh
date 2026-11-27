//go:build windows

package platform

import (
	"context"
	"time"
)

// WatchKey polls GetAsyncKeyState on Windows, where no device file is needed.
func WatchKey(ctx context.Context, key string, fn func()) {
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		d := &windowsDesktop{}
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if d.KeyPressed(key) {
					fn()
					return
				}
			}
		}
	}()
}
