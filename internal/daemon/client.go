package daemon

import (
	"encoding/json"
	"fmt"
	"net"
	"time"
)

// Client talks to the local service from the CLI, the tray or a bar widget.
type Client struct{}

// NewClient creates a client. The endpoint is read on every call, so a
// restarted service is found without keeping stale state.
func NewClient() *Client { return &Client{} }

// Call sends one command and returns the reply.
func (c *Client) Call(req Request) (Response, error) {
	network, address, token, err := readEndpoint()
	if err != nil {
		return Response{}, fmt.Errorf("service is not running: %w", err)
	}
	req.Token = token
	conn, err := net.DialTimeout(network, address, 2*time.Second)
	if err != nil {
		return Response{}, fmt.Errorf("cannot reach service: %w", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return Response{}, err
	}
	var response Response
	if err := json.NewDecoder(conn).Decode(&response); err != nil {
		return Response{}, err
	}
	if response.Error != "" {
		return response, fmt.Errorf("%s", response.Error)
	}
	return response, nil
}

// Running reports whether the local service answers a ping.
func (c *Client) Running() bool {
	_, err := c.Call(Request{Cmd: "ping"})
	return err == nil
}
