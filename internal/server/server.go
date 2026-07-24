// Package server поднимает веб-панель beacon: HTTPS, авторизация, REST API и SSE-стрим метрик.
package server

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"beacon/internal/config"
	"beacon/internal/metrics"
	"beacon/internal/store"
	"beacon/internal/xray"
	"beacon/web"
)

// Server связывает конфиг, хранилище, сбор метрик и Xray в один HTTP-сервис.
type Server struct {
	cfg      *config.Config
	paths    config.Paths
	store    *store.Store
	coll     *metrics.Collector
	xray     *xray.Manager
	sessions *sessionStore
	version  string

	mu     sync.RWMutex
	latest metrics.Sample
}

// New создаёт сервер.
func New(cfg *config.Config, paths config.Paths, st *store.Store, xr *xray.Manager, version string) *Server {
	return &Server{
		cfg:      cfg,
		paths:    paths,
		store:    st,
		xray:     xr,
		coll:     metrics.New(),
		sessions: newSessions(),
		version:  version,
	}
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()

	// панель (статика вшита в бинарник)
	mux.HandleFunc("GET /", s.handleIndex)

	// авторизация
	mux.HandleFunc("POST /api/login", s.handleLogin)
	mux.HandleFunc("POST /api/logout", s.handleLogout)
	mux.HandleFunc("GET /api/session", s.handleSession)

	// данные (требуют авторизации)
	mux.HandleFunc("GET /api/metrics", s.auth(s.handleMetrics))
	mux.HandleFunc("GET /api/stream", s.auth(s.handleStream))
	mux.HandleFunc("GET /api/users", s.auth(s.handleListUsers))
	mux.HandleFunc("POST /api/users", s.auth(s.handleAddUser))
	mux.HandleFunc("PATCH /api/users/{id}", s.auth(s.handleUpdateUser))
	mux.HandleFunc("DELETE /api/users/{id}", s.auth(s.handleDeleteUser))
	mux.HandleFunc("GET /api/users/{id}/link", s.auth(s.handleUserLink))
	mux.HandleFunc("GET /api/users/{id}/qr", s.auth(s.handleUserQR))

	// сервер, логи, смена пароля
	mux.HandleFunc("GET /api/info", s.auth(s.handleInfo))
	mux.HandleFunc("GET /api/logs", s.auth(s.handleLogs))
	mux.HandleFunc("POST /api/password", s.auth(s.handlePassword))

	return mux
}

// Run запускает HTTPS-сервер до отмены ctx.
func (s *Server) Run(ctx context.Context) error {
	cert, err := ensureCert(s.paths)
	if err != nil {
		return err
	}

	go s.sampleLoop(ctx)

	srv := &http.Server{
		Addr:              s.cfg.ListenAddr,
		Handler:           s.routes(),
		TLSConfig:         &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12},
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		<-ctx.Done()
		_ = srv.Close()
	}()

	log.Printf("панель beacon: https://<адрес-сервера>%s", s.cfg.ListenAddr)
	if err := srv.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

// sampleLoop раз в 2 сек кладёт свежий снимок метрик в s.latest.
func (s *Server) sampleLoop(ctx context.Context) {
	s.coll.Sample() // прогрев дельт
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			m := s.coll.Sample()
			s.mu.Lock()
			s.latest = m
			s.mu.Unlock()
		}
	}
}

func (s *Server) getLatest() metrics.Sample {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.latest
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	b, err := web.Files.ReadFile("index.html")
	if err != nil {
		http.Error(w, "панель не найдена", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(b)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
