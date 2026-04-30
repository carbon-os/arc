package webapp

import (
	"fmt"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
)

// Config holds web mode server options.
type Config struct {
	Host    string
	Port    int
	OnReady func()
}

// Server is the web mode HTTP + WebSocket server.
type Server struct {
	cfg   Config
	hubMu sync.Mutex
	hub   map[*websocket.Conn]struct{}
}

// NewServer creates a Server with the given config.
func NewServer(cfg Config) *Server {
	return &Server{cfg: cfg}
}

// Run starts the HTTP server and blocks until it exits.
func (s *Server) Run() error {
	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.Dir("frontend")))
	mux.HandleFunc("/arc-ipc", s.handleWS)

	addr := fmt.Sprintf("%s:%d", s.cfg.Host, s.cfg.Port)
	fmt.Printf("arc: web mode — http://%s\n", addr)

	if s.cfg.OnReady != nil {
		go s.cfg.OnReady()
	}

	return http.ListenAndServe(addr, mux)
}