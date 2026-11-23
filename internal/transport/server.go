package transport

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"sote/internal/peer"
)

// PeerHandler is implemented by the node: it answers what other SOTE nodes
// ask for over the onion service.
type PeerHandler interface {
	Identity(ctx context.Context) (*peer.Identity, error)
	HandleContactRequest(ctx context.Context, r *peer.ContactRequest) error
	HandleMessage(ctx context.Context, e *peer.Envelope) error
}

// Server serves the peer protocol. It is reached only through the onion
// service and must listen on loopback; it exposes nothing but the three peer
// endpoints. (Earlier versions also exposed register, login, add-contact,
// send-message and fetch-messages without authentication, on all interfaces.)
type Server struct {
	handler PeerHandler
	mux     *http.ServeMux
	tlsCfg  *tls.Config

	mu      sync.Mutex
	httpSrv *http.Server
}

// NewServer creates a peer protocol server.
func NewServer(h PeerHandler) *Server {
	s := &Server{handler: h}
	s.registerRoutes()
	return s
}

func (s *Server) WithTLS(cfg *tls.Config) {
	s.tlsCfg = cfg
}

// Handler returns the HTTP handler (for tests).
func (s *Server) Handler() http.Handler { return s.mux }

// Serve accepts connections on ln until Stop is called. ln must be a loopback
// listener (the onion service forwards to it).
func (s *Server) Serve(ln net.Listener) error {
	if addr, ok := ln.Addr().(*net.TCPAddr); ok && !addr.IP.IsLoopback() {
		return fmt.Errorf("refusing to serve the peer protocol on non-loopback address %s", addr)
	}
	srv := &http.Server{
		Handler:           s.mux,
		TLSConfig:         s.tlsCfg,
		ReadHeaderTimeout: 20 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    16 << 10,
		ErrorLog:          nil,
	}
	s.mu.Lock()
	s.httpSrv = srv
	s.mu.Unlock()

	var err error
	if s.tlsCfg != nil {
		err = srv.ServeTLS(ln, "", "")
	} else {
		err = srv.Serve(ln)
	}
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// Shutdown gracefully drains HTTP connections within the context's deadline.
func (s *Server) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	srv := s.httpSrv
	s.mu.Unlock()
	if srv == nil {
		return nil
	}
	return srv.Shutdown(ctx)
}

// Stop performs a graceful HTTP shutdown with a 30s drain timeout.
func (s *Server) Stop() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return s.Shutdown(ctx)
}

func (s *Server) registerRoutes() {
	s.mux = http.NewServeMux()
	s.mux.HandleFunc("GET "+peer.PathIdentity, s.mw(s.handleIdentity))
	s.mux.HandleFunc("POST "+peer.PathContactRequest, s.mw(s.handleContactRequest))
	s.mux.HandleFunc("POST "+peer.PathMessage, s.mw(s.handleMessage))
}

func (s *Server) mw(next http.HandlerFunc) http.HandlerFunc {
	return s.loggingMiddleware(s.recoveryMiddleware(next))
}

type statusWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (sw *statusWriter) WriteHeader(code int) {
	if !sw.wroteHeader {
		sw.status = code
		sw.wroteHeader = true
	}
	sw.ResponseWriter.WriteHeader(code)
}

func (sw *statusWriter) Write(b []byte) (int, error) {
	if !sw.wroteHeader {
		sw.status = http.StatusOK
		sw.wroteHeader = true
	}
	return sw.ResponseWriter.Write(b)
}

func (s *Server) loggingMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next(sw, r)
		slog.Info("peer request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", sw.status,
			"duration_ms", time.Since(start).Milliseconds(),
		)
	}
}

func (s *Server) recoveryMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("panic in handler", "path", r.URL.Path, "panic", rec)
				writeError(w, http.StatusInternalServerError, "internal")
			}
		}()
		next(w, r)
	}
}

// ── handlers ─────────────────────────────────────────────────────────────────

func (s *Server) handleIdentity(w http.ResponseWriter, r *http.Request) {
	id, err := s.handler.Identity(r.Context())
	if err != nil {
		writeProtocolError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, id)
}

func (s *Server) handleContactRequest(w http.ResponseWriter, r *http.Request) {
	var req peer.ContactRequest
	if !decodeBody(w, r, peer.MaxRequestBytes, &req) {
		return
	}
	if err := s.handler.HandleContactRequest(r.Context(), &req); err != nil {
		writeProtocolError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "queued"})
}

func (s *Server) handleMessage(w http.ResponseWriter, r *http.Request) {
	var env peer.Envelope
	if !decodeBody(w, r, peer.MaxEnvelopeBytes, &env) {
		return
	}
	if err := s.handler.HandleMessage(r.Context(), &env); err != nil {
		writeProtocolError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "received"})
}

// decodeBody reads a size-limited JSON body. It writes the error response and
// returns false when the body is too large, not JSON or followed by garbage.
func decodeBody(w http.ResponseWriter, r *http.Request, limit int64, v any) bool {
	if ct := r.Header.Get("Content-Type"); ct != "" && !strings.HasPrefix(ct, "application/json") {
		writeError(w, http.StatusUnsupportedMediaType, "content type must be application/json")
		return false
	}
	body := http.MaxBytesReader(w, r.Body, limit)
	dec := json.NewDecoder(body)
	if err := dec.Decode(v); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "body too large")
		} else {
			writeError(w, http.StatusBadRequest, "invalid JSON")
		}
		return false
	}
	if _, err := dec.Token(); err != io.EOF {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return false
	}
	return true
}

// writeProtocolError maps handler errors to status codes. Details stay in the
// local log; a peer learns only the category.
func writeProtocolError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, peer.ErrInvalid), errors.Is(err, peer.ErrInvalidOnion):
		writeError(w, http.StatusBadRequest, "invalid request")
	case errors.Is(err, peer.ErrBadSignature):
		writeError(w, http.StatusUnauthorized, "bad signature")
	case errors.Is(err, peer.ErrUnknownSender):
		writeError(w, http.StatusForbidden, "not a contact")
	case errors.Is(err, peer.ErrWrongRecipient):
		writeError(w, http.StatusMisdirectedRequest, "wrong recipient")
	case errors.Is(err, peer.ErrTooMany):
		writeError(w, http.StatusTooManyRequests, "too many pending requests")
	case errors.Is(err, peer.ErrNotReady):
		w.Header().Set("Retry-After", "60")
		writeError(w, http.StatusServiceUnavailable, "not ready")
	default:
		slog.Error("peer request failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal")
	}
}

// ── JSON helpers ─────────────────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		slog.Error("write json response", "error", err)
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
