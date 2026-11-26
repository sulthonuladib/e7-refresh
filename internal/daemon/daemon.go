// Package daemon is the control service behind the tray icon, the Omarchy
// bar widget, bar modules on other desktops and the command line.
//
// It speaks one JSON object per line over a per-user local socket (a TCP
// loopback port with a token file on Windows). Nothing in this package needs
// a display, so the same binary can run as a user service with no window.
package daemon

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/sulthonuladib/e7-refresh/internal/atomicfile"
	"github.com/sulthonuladib/e7-refresh/internal/config"
	"github.com/sulthonuladib/e7-refresh/internal/desktop"
	"github.com/sulthonuladib/e7-refresh/internal/engine"
)

// Version is reported by the `status` and CLI commands.
const Version = "0.1.0"

// Request is one command sent to the service.
type Request struct {
	ID     string         `json:"id,omitempty"`
	Token  string         `json:"token"`
	Cmd    string         `json:"cmd"`
	Config *config.Config `json:"config,omitempty"`
	Value  string         `json:"value,omitempty"`
}

// Response is the reply to one command. ID echoes the request so bar
// widgets can have several calls in flight.
type Response struct {
	ID      string           `json:"id,omitempty"`
	OK      bool             `json:"ok"`
	Error   string           `json:"error,omitempty"`
	Status  *engine.Stats    `json:"status,omitempty"`
	Config  *config.Config   `json:"config,omitempty"`
	Desktop *desktop.Info    `json:"desktop,omitempty"`
	Info    map[string]string `json:"info,omitempty"`
}

// Server owns the socket and the refresh engine of one process.
type Server struct {
	engine   *engine.Engine
	listener net.Listener
	token    string
	mu       sync.RWMutex
	open     func()
	quit     func()
}

// Start binds the local socket and starts accepting commands.
func Start(e *engine.Engine) (*Server, error) {
	tokenBytes := make([]byte, 24)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, err
	}
	token := hex.EncodeToString(tokenBytes)
	listener, err := listen(token)
	if err != nil {
		return nil, err
	}
	s := &Server{engine: e, listener: listener, token: token}
	go s.accept()
	return s, nil
}

// SetOpenFunc attaches the tray's "show window" action, when there is one.
func (s *Server) SetOpenFunc(fn func()) {
	s.mu.Lock()
	s.open = fn
	s.mu.Unlock()
}

// SetQuitFunc attaches the tray's quit action, when there is one.
func (s *Server) SetQuitFunc(fn func()) {
	s.mu.Lock()
	s.quit = fn
	s.mu.Unlock()
}

// Close stops accepting commands.
func (s *Server) Close() error {
	_ = os.Remove(endpointFile())
	return s.listener.Close()
}

// Serve runs the service until ctx is canceled. It writes no GUI.
func Serve(ctx context.Context, e *engine.Engine) error {
	s, err := Start(e)
	if err != nil {
		return err
	}
	defer s.Close()
	<-ctx.Done()
	e.Stop()
	e.Wait()
	return nil
}

func (s *Server) accept() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		go s.handle(conn)
	}
}

func (s *Server) handle(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	reader := bufio.NewReaderSize(conn, 64*1024)
	line, err := reader.ReadString('\n')
	if err != nil {
		return
	}
	var req Request
	if err := json.Unmarshal([]byte(line), &req); err != nil {
		write(conn, Response{Error: "invalid request: " + err.Error()})
		return
	}
	if req.Token != s.token {
		write(conn, Response{Error: "unauthorized"})
		return
	}
	response := s.dispatch(req)
	response.ID = req.ID
	write(conn, response)
}

func (s *Server) dispatch(req Request) Response {
	switch req.Cmd {
	case "ping":
		return Response{OK: true}
	case "status":
		status := s.engine.Status()
		return Response{OK: true, Status: &status}
	case "start":
		cfg, err := requestConfig(req)
		if err != nil {
			return Response{Error: err.Error()}
		}
		if err := s.engine.Start(cfg); err != nil {
			return Response{Error: err.Error()}
		}
		return Response{OK: true}
	case "stop":
		if err := s.engine.Stop(); err != nil {
			return Response{Error: err.Error()}
		}
		return Response{OK: true}
	case "get-config":
		cfg, err := config.Load()
		if err != nil {
			return Response{Error: err.Error()}
		}
		return Response{OK: true, Config: &cfg}
	case "set-config":
		if req.Config == nil {
			return Response{Error: "set-config needs a config"}
		}
		if err := config.Save(*req.Config); err != nil {
			return Response{Error: err.Error()}
		}
		s.engine.SetConfig(*req.Config)
		return Response{OK: true}
	case "detect":
		info := desktop.Detect()
		return Response{OK: true, Desktop: &info}
	case "history":
		return Response{OK: true, Info: map[string]string{"dir": config.HistoryDir()}}
	case "open":
		s.mu.RLock()
		fn := s.open
		s.mu.RUnlock()
		if fn == nil {
			return Response{Error: "no window attached to this service"}
		}
		fn()
		return Response{OK: true}
	case "quit":
		s.mu.RLock()
		fn := s.quit
		s.mu.RUnlock()
		if fn == nil {
			return Response{Error: "no GUI attached to this service"}
		}
		fn()
		return Response{OK: true}
	case "version":
		return Response{OK: true, Info: map[string]string{"version": Version}}
	default:
		return Response{Error: fmt.Sprintf("unknown command %q", req.Cmd)}
	}
}

func requestConfig(req Request) (config.Config, error) {
	if req.Config != nil {
		return *req.Config, nil
	}
	return config.Load()
}

func write(conn net.Conn, response Response) {
	_ = json.NewEncoder(conn).Encode(response)
}

func endpointFile() string {
	return filepath.Join(config.RuntimeDir(), "endpoint")
}

func writeEndpoint(network, address, token string) error {
	dir := config.RuntimeDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	content := fmt.Sprintf("%s\n%s\n%s\n", network, address, token)
	tmp, err := os.CreateTemp(dir, ".endpoint-")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return atomicfile.Replace(name, endpointFile())
}

func readEndpoint() (network, address, token string, err error) {
	data, err := os.ReadFile(endpointFile())
	if err != nil {
		return "", "", "", err
	}
	lines := splitLines(string(data))
	if len(lines) < 3 {
		return "", "", "", fmt.Errorf("invalid endpoint file")
	}
	return lines[0], lines[1], lines[2], nil
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	return lines
}
