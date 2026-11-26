//go:build windows

package daemon

import "net"

// listen uses a loopback port on Windows. The random port and the secret are
// written to a file in the user's state directory, so other users and remote
// clients cannot drive this service.
func listen(token string) (net.Listener, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	if err := writeEndpoint("tcp", listener.Addr().String(), token); err != nil {
		listener.Close()
		return nil, err
	}
	return listener, nil
}
