// Package mobile exposes the embedded Go HTTP service to Android through gomobile.
package mobile

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rusik69/fp/internal/httpapi"
)


// Server owns the loopback HTTP server used by the Android WebView.
type Server struct {
	dataDir string
	port    int
	mu      sync.Mutex
	server  *http.Server
	url     string
}

// NewServer creates an embedded Go server. Pass port 0 to select a free port in tests;
// Android uses port 8080 so its same-origin PWA cache survives app restarts.
func NewServer(dataDir string, port int) *Server {
	return &Server{dataDir: dataDir, port: port}
}

// Start starts the server on loopback and returns its base URL.
func (s *Server) Start() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.server != nil {
		return s.url, nil
	}
	if strings.TrimSpace(s.dataDir) == "" {
		return "", errors.New("Android app data directory is required")
	}
	if s.port < 0 || s.port > 65535 {
		return "", errors.New("port must be between 0 and 65535")
	}
	dataDir, err := filepath.Abs(s.dataDir)
	if err != nil {
		return "", fmt.Errorf("resolve Android app data directory: %w", err)
	}
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return "", fmt.Errorf("create Android app data directory: %w", err)
	}
	if err := os.Setenv("FP_DATA_DIR", dataDir); err != nil {
		return "", fmt.Errorf("set Android app data directory: %w", err)
	}
	listener, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(s.port)))
	if err != nil {
		return "", fmt.Errorf("listen on Android loopback: %w", err)
	}
	server := &http.Server{Handler: httpapi.NewHandler(), ReadHeaderTimeout: 5 * time.Second}
	s.server = server
	s.url = "http://localhost:" + strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
	go func() { _ = server.Serve(listener) }()
	return s.url, nil
}

// Stop shuts down the server. Calling Stop repeatedly is safe.
func (s *Server) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.server == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.server.Shutdown(ctx); err != nil {
		return fmt.Errorf("stop Android loopback server: %w", err)
	}
	s.server = nil
	s.url = ""
	return nil
}
