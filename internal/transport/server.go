package transport

import (
	"context"
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"sote/internal/auth"
	"sote/internal/contacts"
	"sote/internal/domain"
	"sote/internal/messages"
)

type Server struct {
	auth     *auth.AuthService
	contacts *contacts.Service
	messages *messages.Service
	tor      domain.TorService
	db       *sql.DB
	mux      *http.ServeMux
	httpSrv  *http.Server
	addr     string
	tlsCfg   *tls.Config

	currentUsername string
	currentPassword string
	mu              sync.RWMutex
}

func NewServer(authSvc *auth.AuthService, contactSvc *contacts.Service, msgSvc *messages.Service, torSvc domain.TorService, db *sql.DB, addr string) *Server {
	s := &Server{
		auth:     authSvc,
		contacts: contactSvc,
		messages: msgSvc,
		tor:      torSvc,
		db:       db,
		addr:     addr,
	}

	s.initContactRequestsTable()
	s.registerRoutes()

	return s
}

func (s *Server) WithTLS(cfg *tls.Config) {
	s.tlsCfg = cfg
}

func (s *Server) Mux() *http.ServeMux {
	return s.mux
}

func (s *Server) Start() error {
	s.httpSrv = &http.Server{
		Addr:    s.addr,
		Handler: s.mux,
		TLSConfig: s.tlsCfg,
	}
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	go func() {
		if s.tlsCfg != nil {
			s.httpSrv.ServeTLS(ln, "", "")
		} else {
			s.httpSrv.Serve(ln)
		}
	}()
	return nil
}

// Shutdown gracefully drains HTTP connections within the given context's deadline
// and then stops accepting new connections. Callers should provide a context
// with an appropriate timeout (30s recommended for graceful drain).
func (s *Server) Shutdown(ctx context.Context) error {
	if s.httpSrv == nil {
		return nil
	}
	return s.httpSrv.Shutdown(ctx)
}

// Stop performs a graceful HTTP shutdown with a 30s drain timeout.
func (s *Server) Stop() error {
	if s.httpSrv == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return s.httpSrv.Shutdown(ctx)
}

func (s *Server) registerRoutes() {
	s.mux = http.NewServeMux()
	s.mux.HandleFunc("POST /register", s.mw(s.handleRegister))
	s.mux.HandleFunc("POST /login", s.mw(s.handleLogin))
	s.mux.HandleFunc("GET /onion-address", s.mw(s.handleOnionAddress))
	s.mux.HandleFunc("POST /add-contact", s.mw(s.handleAddContact))
	s.mux.HandleFunc("POST /receive-contact-request", s.mw(s.handleReceiveContactRequest))
	s.mux.HandleFunc("POST /send-message", s.mw(s.handleSendMessage))
	s.mux.HandleFunc("POST /receive-message", s.mw(s.handleReceiveMessage))
	s.mux.HandleFunc("POST /fetch-messages", s.mw(s.handleFetchMessages))
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
		slog.Info("http request",
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
				http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
			}
		}()
		next(w, r)
	}
}

// ── handlers ─────────────────────────────────────────────────────────────────

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.Username == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "username and password required")
		return
	}

	user, err := s.auth.Register(r.Context(), req.Username, req.Password)
	if err != nil {
		switch err {
		case auth.ErrInvalidUsername, auth.ErrWeakPassword:
			writeError(w, http.StatusBadRequest, err.Error())
		case auth.ErrDuplicateUser:
			writeError(w, http.StatusConflict, err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "registration failed")
		}
		return
	}

	s.mu.Lock()
	s.currentUsername = user.Username
	s.currentPassword = user.RawPassword
	s.mu.Unlock()

	writeJSON(w, http.StatusCreated, map[string]any{
		"username":       user.Username,
		"public_key":     string(user.PublicKey),
		"onion_address":  user.OnionAddress,
	})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.Username == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "username and password required")
		return
	}

	user, err := s.auth.Login(r.Context(), req.Username, req.Password)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid username or password")
		return
	}

	s.mu.Lock()
	s.currentUsername = user.Username
	s.currentPassword = user.RawPassword
	s.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{
		"username":       user.Username,
		"public_key":     string(user.PublicKey),
		"onion_address":  user.OnionAddress,
	})
}

func (s *Server) handleOnionAddress(w http.ResponseWriter, r *http.Request) {
	addr := s.tor.GetOnionAddress()
	writeJSON(w, http.StatusOK, map[string]string{
		"onion_address": addr,
	})
}

func (s *Server) handleAddContact(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username        string `json:"username"`
		ContactUsername string `json:"contact_username"`
		OnionAddress    string `json:"onion_address"`
		PublicKey       string `json:"public_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.Username == "" || req.ContactUsername == "" || req.OnionAddress == "" || req.PublicKey == "" {
		writeError(w, http.StatusBadRequest, "all fields required: username, contact_username, onion_address, public_key")
		return
	}

	contact := domain.Contact{
		Username:     req.ContactUsername,
		OnionAddress: req.OnionAddress,
		PublicKey:    []byte(req.PublicKey),
	}

	if err := s.contacts.AddContact(r.Context(), req.Username, contact); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleReceiveContactRequest(w http.ResponseWriter, r *http.Request) {
	var req struct {
		FromUsername  string `json:"from_username"`
		FromOnion     string `json:"from_onion"`
		FromPublicKey string `json:"from_public_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.FromUsername == "" || req.FromOnion == "" || req.FromPublicKey == "" {
		writeError(w, http.StatusBadRequest, "all fields required: from_username, from_onion, from_public_key")
		return
	}

	if err := s.saveContactRequest(req.FromUsername, req.FromOnion, []byte(req.FromPublicKey)); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to queue contact request")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "queued"})
}

func (s *Server) handleSendMessage(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Sender   string `json:"sender"`
		Receiver string `json:"receiver"`
		Password string `json:"password"`
		Message  string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.Sender == "" || req.Receiver == "" || req.Password == "" || req.Message == "" {
		writeError(w, http.StatusBadRequest, "all fields required: sender, receiver, password, message")
		return
	}

	if err := s.messages.SendMessage(r.Context(), req.Sender, req.Receiver, req.Password, req.Message); err != nil {
		switch err {
		case messages.ErrMessageTooLarge:
			writeError(w, http.StatusBadRequest, err.Error())
		default:
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "sent"})
}

func (s *Server) handleReceiveMessage(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Sender           string `json:"sender"`
		Receiver         string `json:"receiver"`
		EncryptedContent string `json:"encrypted_content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.Sender == "" || req.Receiver == "" || req.EncryptedContent == "" {
		writeError(w, http.StatusBadRequest, "all fields required: sender, receiver, encrypted_content")
		return
	}

	if err := s.messages.ReceiveMessage(r.Context(), req.Sender, req.Receiver, []byte(req.EncryptedContent)); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
}

func (s *Server) handleFetchMessages(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username  string `json:"username"`
		OtherUser string `json:"other_user"`
		Password  string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.Username == "" || req.OtherUser == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "all fields required: username, other_user, password")
		return
	}

	msgs, err := s.messages.FetchMessages(r.Context(), req.Username, req.OtherUser, req.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	type msgResponse struct {
		Sender    string `json:"sender"`
		Receiver  string `json:"receiver"`
		Content   string `json:"content"`
		Timestamp string `json:"timestamp"`
	}

	out := make([]msgResponse, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, msgResponse{
			Sender:    m.Sender,
			Receiver:  m.Receiver,
			Content:   m.Content,
			Timestamp: m.Timestamp,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"messages": out,
	})
}

// ── contact request queue ────────────────────────────────────────────────────

func (s *Server) initContactRequestsTable() {
	_, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS contact_requests (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		from_username TEXT NOT NULL,
		from_onion TEXT NOT NULL,
		from_public_key BLOB NOT NULL,
		status TEXT NOT NULL DEFAULT 'pending',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`)
	if err != nil {
		slog.Error("failed to create contact_requests table", "error", err)
	}
}

func (s *Server) saveContactRequest(fromUsername, fromOnion string, fromPublicKey []byte) error {
	stmt, err := s.db.Prepare(`INSERT INTO contact_requests (from_username, from_onion, from_public_key) VALUES (?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare: %w", err)
	}
	defer stmt.Close()

	_, err = stmt.Exec(fromUsername, fromOnion, fromPublicKey)
	if err != nil {
		return fmt.Errorf("insert: %w", err)
	}
	return nil
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
