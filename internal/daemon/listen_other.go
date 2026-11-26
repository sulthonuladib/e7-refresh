//go:build !linux && !windows

package daemon

import (
	"net"
	"os"
	"path/filepath"

	"github.com/sulthonuladib/e7-refresh/internal/config"
)

func listen(token string) (net.Listener, error) {
	dir := config.RuntimeDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "e7-refresh.sock")
	_ = os.Remove(path)
	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	_ = os.Chmod(path, 0o600)
	if err := writeEndpoint("unix", path, token); err != nil {
		listener.Close()
		return nil, err
	}
	return listener, nil
}
