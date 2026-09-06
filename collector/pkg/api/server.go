package api

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/Whatnamed/ProxyLens/collector/pkg/storage"
)

type ServerConfig struct {
	DB             *sql.DB
	DBPath         string
	ListenAddr     string
	Token          string
	AllowedOrigins []string
	AppVersion     string
}

type Server struct {
	db              *sql.DB
	dbPath          string
	listenAddr      string
	token           string
	allowedOrigins  map[string]bool
	appVersion      string
	httpServer      *http.Server
	listener        net.Listener
	analyticsSvc    *storage.AnalyticsService
	querySvc        *storage.QueryService
	intelligenceSvc *storage.AuditIntelligenceService

	mu      sync.Mutex
	running bool
}

func NewServer(cfg ServerConfig) (*Server, error) {
	if cfg.Token == "" {
		return nil, fmt.Errorf("token cannot be empty")
	}
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = "127.0.0.1:0"
	}
	if cfg.AppVersion == "" {
		cfg.AppVersion = "0.7.0-phase3a"
	}

	originsMap := make(map[string]bool)
	for _, o := range cfg.AllowedOrigins {
		originsMap[o] = true
	}

	s := &Server{
		db:             cfg.DB,
		dbPath:         cfg.DBPath,
		listenAddr:     cfg.ListenAddr,
		token:          cfg.Token,
		allowedOrigins: originsMap,
		appVersion:     cfg.AppVersion,
	}

	if cfg.DB != nil {
		s.analyticsSvc = storage.NewAnalyticsService(cfg.DB)
		s.querySvc = storage.NewQueryService(cfg.DB)
		s.intelligenceSvc = storage.NewAuditIntelligenceService(cfg.DB)
	}

	return s, nil
}

func (s *Server) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return fmt.Errorf("server already running")
	}

	ln, err := net.Listen("tcp", s.listenAddr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", s.listenAddr, err)
	}
	s.listener = ln

	mux := http.NewServeMux()
	s.registerRoutes(mux)

	s.httpServer = &http.Server{
		Handler:      s.AuthAndCORSMiddleware(mux),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	s.running = true

	go func() {
		if err := s.httpServer.Serve(ln); err != nil && err != http.ErrServerClosed {
			// server closed unexpectedly
		}
	}()

	return nil
}

func (s *Server) Addr() net.Addr {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listener != nil {
		return s.listener.Addr()
	}
	return nil
}

func (s *Server) Port() int {
	addr := s.Addr()
	if addr != nil {
		if tcpAddr, ok := addr.(*net.TCPAddr); ok {
			return tcpAddr.Port
		}
	}
	return 0
}

func (s *Server) Stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return nil
	}

	s.running = false
	if s.httpServer != nil {
		return s.httpServer.Shutdown(ctx)
	}
	return nil
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.Stop(ctx)
}

func (s *Server) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/healthz", s.handleHealthz)
	mux.HandleFunc("/api/v1/meta", s.handleMeta)
	mux.HandleFunc("/api/v1/analytics/summary", s.handleAnalyticsSummary)
	mux.HandleFunc("/api/v1/analytics/top/processes", s.handleTopProcesses)
	mux.HandleFunc("/api/v1/analytics/top/hosts", s.handleTopHosts)
	mux.HandleFunc("/api/v1/analytics/top/rules", s.handleTopRules)
	mux.HandleFunc("/api/v1/analytics/top/final-proxies", s.handleTopFinalProxies)
	mux.HandleFunc("/api/v1/analytics/protocols", s.handleProtocols)
	mux.HandleFunc("/api/v1/coverage", s.handleCoverage)
	mux.HandleFunc("/api/v1/intelligence/findings", s.handleIntelligenceFindings)
	mux.HandleFunc("/api/v1/connections", s.handleConnections)
	mux.HandleFunc("/api/v1/connections/", s.handleConnectionDetailRouter)
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
