package transport

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"sote/internal/auth"
	"sote/internal/contacts"
	"sote/internal/crypto"
	"sote/internal/messages"
	"sote/internal/storage"
)

type mockTorService struct {
	onionAddr string
}

func (m *mockTorService) Start(_ context.Context) error { return nil }
func (m *mockTorService) Stop() error                   { return nil }
func (m *mockTorService) GetOnionAddress() string       { return m.onionAddr }

func setupTestServer(t *testing.T) *Server {
	t.Helper()

	db, err := storage.Initialize(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	authSvc := auth.New(db)
	contactSvc := contacts.NewService(db)
	cryptoSvc := crypto.New()
	msgSvc := messages.NewService(db, cryptoSvc)
	torSvc := &mockTorService{onionAddr: "test123456.onion"}

	return NewServer(authSvc, contactSvc, msgSvc, torSvc, db, "127.0.0.1:0")
}

func generateTestPublicKey(t *testing.T) string {
	t.Helper()
	c := crypto.New()
	_, pub, err := c.GenerateKeyPair()
	require.NoError(t, err)
	return string(pub)
}

func doRequest(srv *Server, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	return w
}

func jsonBody(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func TestRegisterHandler(t *testing.T) {
	srv := setupTestServer(t)

	w := doRequest(srv, "POST", "/register", `{"username":"alice","password":"secret123"}`)

	assert.Equal(t, http.StatusCreated, w.Code)
	assert.Contains(t, w.Body.String(), `"username":"alice"`)
	assert.Contains(t, w.Body.String(), `"public_key"`)
	assert.Contains(t, w.Header().Get("Content-Type"), "application/json")
}

func TestRegisterDuplicate(t *testing.T) {
	srv := setupTestServer(t)

	body := `{"username":"bob","password":"secret123"}`
	assert.Equal(t, http.StatusCreated, doRequest(srv, "POST", "/register", body).Code)
	assert.Equal(t, http.StatusConflict, doRequest(srv, "POST", "/register", body).Code)
}

func TestRegisterWeakPassword(t *testing.T) {
	srv := setupTestServer(t)

	w := doRequest(srv, "POST", "/register", `{"username":"eve","password":"short"}`)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "password must be at least 8 characters")
}

func TestLoginHandler(t *testing.T) {
	srv := setupTestServer(t)

	doRequest(srv, "POST", "/register", `{"username":"alice","password":"secret123"}`)

	w := doRequest(srv, "POST", "/login", `{"username":"alice","password":"secret123"}`)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"username":"alice"`)
}

func TestLoginWrongPassword(t *testing.T) {
	srv := setupTestServer(t)

	doRequest(srv, "POST", "/register", `{"username":"alice","password":"secret123"}`)

	w := doRequest(srv, "POST", "/login", `{"username":"alice","password":"wrongpass"}`)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Contains(t, w.Body.String(), "invalid username or password")
}

func TestLoginNonExistent(t *testing.T) {
	srv := setupTestServer(t)

	w := doRequest(srv, "POST", "/login", `{"username":"nobody","password":"secret123"}`)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestInvalidJSON(t *testing.T) {
	srv := setupTestServer(t)

	w := doRequest(srv, "POST", "/register", `not json`)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "invalid JSON")
}

func TestMissingFields(t *testing.T) {
	srv := setupTestServer(t)

	w := doRequest(srv, "POST", "/register", `{"password":"secret123"}`)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestMissingFieldsLogin(t *testing.T) {
	srv := setupTestServer(t)

	w := doRequest(srv, "POST", "/login", `{"username":"alice"}`)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestOnionAddressHandler(t *testing.T) {
	srv := setupTestServer(t)

	w := doRequest(srv, "GET", "/onion-address", "")
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"onion_address":"test123456.onion"`)
}

func TestAddContactHandler(t *testing.T) {
	srv := setupTestServer(t)
	key := generateTestPublicKey(t)

	doRequest(srv, "POST", "/register", `{"username":"alice","password":"secret123"}`)

	body := jsonBody(map[string]string{
		"username":         "alice",
		"contact_username": "bob",
		"onion_address":    "bob.onion",
		"public_key":       key,
	})
	w := doRequest(srv, "POST", "/add-contact", body)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"status":"ok"`)
}

func TestAddContactMissingFields(t *testing.T) {
	srv := setupTestServer(t)

	w := doRequest(srv, "POST", "/add-contact", `{"username":"alice"}`)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestContactRequestQueue(t *testing.T) {
	srv := setupTestServer(t)

	doRequest(srv, "POST", "/register", `{"username":"alice","password":"secret123"}`)

	body := `{"from_username":"bob","from_onion":"bob.onion","from_public_key":"bobs-public-key"}`
	w := doRequest(srv, "POST", "/receive-contact-request", body)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"status":"queued"`)

	var count int
	err := srv.db.QueryRow("SELECT COUNT(*) FROM contact_requests WHERE from_username = ?", "bob").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestContactRequestQueueMissingFields(t *testing.T) {
	srv := setupTestServer(t)

	w := doRequest(srv, "POST", "/receive-contact-request", `{"from_username":"bob"}`)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestSendMessageHandler(t *testing.T) {
	srv := setupTestServer(t)
	key := generateTestPublicKey(t)

	doRequest(srv, "POST", "/register", `{"username":"alice","password":"secret123"}`)
	addBody := jsonBody(map[string]string{
		"username":         "alice",
		"contact_username": "bob",
		"onion_address":    "bob.onion",
		"public_key":       key,
	})
	doRequest(srv, "POST", "/add-contact", addBody)

	body := `{"sender":"alice","receiver":"bob","password":"secret123","message":"Hello Bob"}`
	w := doRequest(srv, "POST", "/send-message", body)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"status":"sent"`)
}

func TestSendMessageMissingFields(t *testing.T) {
	srv := setupTestServer(t)

	w := doRequest(srv, "POST", "/send-message", `{"sender":"alice"}`)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestReceiveMessageHandler(t *testing.T) {
	srv := setupTestServer(t)
	key := generateTestPublicKey(t)

	doRequest(srv, "POST", "/register", `{"username":"alice","password":"secret123"}`)
	addBody := jsonBody(map[string]string{
		"username":         "alice",
		"contact_username": "bob",
		"onion_address":    "bob.onion",
		"public_key":       key,
	})
	doRequest(srv, "POST", "/add-contact", addBody)

	body := `{"sender":"bob","receiver":"alice","encrypted_content":"some-encrypted-data"}`
	w := doRequest(srv, "POST", "/receive-message", body)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"status":"saved"`)

	rows, err := storage.GetMessages(srv.db, "alice", "bob")
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(rows), 1)
}

func TestFetchMessagesHandler(t *testing.T) {
	srv := setupTestServer(t)
	key := generateTestPublicKey(t)

	doRequest(srv, "POST", "/register", `{"username":"alice","password":"secret123"}`)
	addBody := jsonBody(map[string]string{
		"username":         "alice",
		"contact_username": "bob",
		"onion_address":    "bob.onion",
		"public_key":       key,
	})
	doRequest(srv, "POST", "/add-contact", addBody)

	doRequest(srv, "POST", "/send-message", `{"sender":"alice","receiver":"bob","password":"secret123","message":"Hi"}`)

	w := doRequest(srv, "POST", "/fetch-messages", `{"username":"alice","other_user":"bob","password":"secret123"}`)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"messages"`)
}

func TestFetchMessagesMissingFields(t *testing.T) {
	srv := setupTestServer(t)

	w := doRequest(srv, "POST", "/fetch-messages", `{"username":"alice"}`)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRecoveryMiddleware(t *testing.T) {
	srv := setupTestServer(t)

	srv.mux.HandleFunc("GET /panic", srv.mw(func(w http.ResponseWriter, r *http.Request) {
		panic("test panic")
	}))

	w := doRequest(srv, "GET", "/panic", "")
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "internal server error")
}

func TestContentTypeJSON(t *testing.T) {
	srv := setupTestServer(t)

	w := doRequest(srv, "GET", "/onion-address", "")
	assert.Contains(t, w.Header().Get("Content-Type"), "application/json")
}
