//go:build !linux && !windows

package platform

import "context"

// WatchKey is unsupported on other platforms.
func WatchKey(context.Context, string, func()) {}
