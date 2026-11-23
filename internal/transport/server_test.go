package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"sote/internal/peer"
)

type fakeHandler struct {
	err      error
	panicMsg string
	gotMsg   *peer.Envelope
	gotReq   *peer.ContactRequest
}

func (f *fakeHandler) Identity(context.Context) (*peer.Identity, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &peer.Identity{Version: 1, Username: "alice", Onion: "x.onion", PublicKey: "KEY"}, nil
}

func (f *fakeHandler) HandleContactRequest(_ context.Context, r *peer.ContactRequest) error {
	f.gotReq = r
	return f.err
}

func (f *fakeHandler) HandleMessage(_ context.Context, e *peer.Envelope) error {
	if f.panicMsg != "" {
		panic(f.panicMsg)
	}
	f.gotMsg = e
	return f.err
}

func do(t *testing.T, s *Server, method, path, contentType string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

func TestIdentityEndpoint(t *testing.T) {
	s := NewServer(&fakeHandler{})
	rec := do(t, s, http.MethodGet, peer.PathIdentity, "", nil)
	require.Equal(t, http.StatusOK, rec.Code)
	var id peer.Identity
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &id))
	assert.Equal(t, "alice", id.Username)

	assert.Equal(t, http.StatusMethodNotAllowed, do(t, s, http.MethodPost, peer.PathIdentity, "application/json", []byte("{}")).Code)
}

func TestOnlyPeerEndpointsAreServed(t *testing.T) {
	// The local API that used to be reachable by anyone (register, login,
	// add-contact, send-message, fetch-messages, onion-address) is gone.
	s := NewServer(&fakeHandler{})
	for _, path := range []string{"/register", "/login", "/add-contact", "/send-message", "/fetch-messages", "/onion-address", "/"} {
		rec := do(t, s, http.MethodPost, path, "application/json", []byte(`{}`))
		assert.Equal(t, http.StatusNotFound, rec.Code, path)
		rec = do(t, s, http.MethodGet, path, "", nil)
		assert.Equal(t, http.StatusNotFound, rec.Code, path)
	}
}

func TestBodyHandling(t *testing.T) {
	h := &fakeHandler{}
	s := NewServer(h)

	rec := do(t, s, http.MethodPost, peer.PathMessage, "application/json", []byte(`{"version":1,"message_id":"abc"}`))
	assert.Equal(t, http.StatusOK, rec.Code)
	require.NotNil(t, h.gotMsg)
	assert.Equal(t, "abc", h.gotMsg.MessageID)

	assert.Equal(t, http.StatusBadRequest, do(t, s, http.MethodPost, peer.PathMessage, "application/json", []byte(`not json`)).Code)
	assert.Equal(t, http.StatusBadRequest, do(t, s, http.MethodPost, peer.PathMessage, "application/json", []byte(`{} {}`)).Code, "trailing data")
	assert.Equal(t, http.StatusUnsupportedMediaType, do(t, s, http.MethodPost, peer.PathMessage, "text/plain", []byte(`{}`)).Code)

	huge := []byte(`{"payload":"` + strings.Repeat("A", peer.MaxEnvelopeBytes) + `"}`)
	assert.Equal(t, http.StatusRequestEntityTooLarge, do(t, s, http.MethodPost, peer.PathMessage, "application/json", huge).Code)
	bigRequest := []byte(`{"from_public_key":"` + strings.Repeat("A", peer.MaxRequestBytes) + `"}`)
	assert.Equal(t, http.StatusRequestEntityTooLarge, do(t, s, http.MethodPost, peer.PathContactRequest, "application/json", bigRequest).Code)

	rec = do(t, s, http.MethodPost, peer.PathContactRequest, "application/json", []byte(`{"from_username":"bob"}`))
	assert.Equal(t, http.StatusAccepted, rec.Code)
	assert.Equal(t, "bob", h.gotReq.FromUsername)
}

func TestErrorsMapToStatusCodes(t *testing.T) {
	cases := []struct {
		err  error
		code int
	}{
		{fmt.Errorf("%w: x", peer.ErrInvalid), http.StatusBadRequest},
		{peer.ErrBadSignature, http.StatusUnauthorized},
		{peer.ErrUnknownSender, http.StatusForbidden},
		{peer.ErrWrongRecipient, http.StatusMisdirectedRequest},
		{peer.ErrTooMany, http.StatusTooManyRequests},
		{peer.ErrNotReady, http.StatusServiceUnavailable},
		{errors.New("disk on fire at /home/user/.sote/data.db"), http.StatusInternalServerError},
	}
	for _, c := range cases {
		s := NewServer(&fakeHandler{err: c.err})
		rec := do(t, s, http.MethodPost, peer.PathMessage, "application/json", []byte(`{}`))
		assert.Equal(t, c.code, rec.Code, c.err.Error())
		assert.NotContains(t, rec.Body.String(), "/home/user", "internal details stay local")
		if c.code == http.StatusServiceUnavailable {
			assert.NotEmpty(t, rec.Header().Get("Retry-After"))
		}
	}
}

func TestPanicIsRecovered(t *testing.T) {
	s := NewServer(&fakeHandler{panicMsg: "boom"})
	rec := do(t, s, http.MethodPost, peer.PathMessage, "application/json", []byte(`{}`))
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.NotContains(t, rec.Body.String(), "boom")
}

func TestServeRefusesNonLoopbackListener(t *testing.T) {
	ln, err := net.Listen("tcp", "0.0.0.0:0")
	require.NoError(t, err)
	defer ln.Close()
	err = NewServer(&fakeHandler{}).Serve(ln)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "non-loopback")
}

func TestServeAndShutdown(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	s := NewServer(&fakeHandler{})
	done := make(chan error, 1)
	go func() { done <- s.Serve(ln) }()

	require.Eventually(t, func() bool {
		resp, err := http.Get("http://" + ln.Addr().String() + peer.PathIdentity)
		if err != nil {
			return false
		}
		resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	}, 5e9, 1e7)

	require.NoError(t, s.Stop())
	assert.NoError(t, <-done)
}
