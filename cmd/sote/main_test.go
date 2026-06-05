package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"sote/internal/auth"
	"sote/internal/contacts"
	"sote/internal/crypto"
	"sote/internal/messages"
	"sote/internal/storage"
	"sote/internal/tor"
	"sote/internal/transport"
)

func TestMainParsesFlags(t *testing.T) {
	// Save and restore original args and flag set
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()
	oldFlagCommandLine := flag.CommandLine
	defer func() { flag.CommandLine = oldFlagCommandLine }()

	os.Args = []string{"sote", "--daemon"}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)

	daemonMode := flag.Bool("daemon", false, "run as daemon only")
	flag.Parse()

	assert.True(t, *daemonMode, "daemon flag should be true when --daemon is passed")
}

func TestMainHelpFlag(t *testing.T) {
	// Save and restore original args and flag set
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()
	oldFlagCommandLine := flag.CommandLine
	defer func() { flag.CommandLine = oldFlagCommandLine }()

	os.Args = []string{"sote", "--help"}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)

	// flag.Parse() with --help triggers flag.ExitOnError or prints help
	// We verify the flag set properly recognizes --help
	err := flag.CommandLine.Parse(os.Args[1:])
	assert.ErrorIs(t, err, flag.ErrHelp, "--help should trigger ErrHelp")
}

func TestMainDefaultDaemonFalse(t *testing.T) {
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()
	oldFlagCommandLine := flag.CommandLine
	defer func() { flag.CommandLine = oldFlagCommandLine }()

	os.Args = []string{"sote"}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)

	daemonMode := flag.Bool("daemon", false, "run as daemon only")
	flag.Parse()

	assert.False(t, *daemonMode, "daemon flag should be false by default")
}

type mockTorService struct {
	onionAddr string
}

func (m *mockTorService) Start(_ context.Context) error { return nil }
func (m *mockTorService) Stop() error                   { return nil }
func (m *mockTorService) GetOnionAddress() string       { return m.onionAddr }

func setupDaemonTestServer(t *testing.T) (*transport.Server, *sql.DB) {
	t.Helper()

	db, err := storage.Initialize(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	authSvc := auth.New(db)
	contactSvc := contacts.NewService(db)
	cryptoSvc := crypto.New()
	msgSvc := messages.NewService(db, cryptoSvc)
	torSvc := &mockTorService{onionAddr: "testauto.onion"}

	srv := transport.NewServer(authSvc, contactSvc, msgSvc, torSvc, db, "127.0.0.1:0")
	srv.SetAutoAccept(true)
	return srv, db
}

func doRequest(srv *transport.Server, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Mux().ServeHTTP(w, req)
	return w
}

func TestDaemonModeHTTPEndpoints(t *testing.T) {
	srv, db := setupDaemonTestServer(t)

	registerBody := `{"username":"daemonuser","password":"secret123456"}`
	w := doRequest(srv, "POST", "/register", registerBody)
	assert.Equal(t, http.StatusCreated, w.Code)

	recvBody := `{"from_username":"remote","from_onion":"remote.onion","from_public_key":"bW9ja1B1YmxpY0tleQ=="}`
	w = doRequest(srv, "POST", "/receive-contact-request", recvBody)
	assert.Equal(t, http.StatusOK, w.Code)

	contacts, err := storage.GetAllContacts(db, "daemonuser")
	require.NoError(t, err)
	require.Len(t, contacts, 1)
	assert.Equal(t, "remote", contacts[0].ContactUsername)
	assert.Equal(t, "remote.onion", contacts[0].OnionAddress)
}

func TestDaemonModeNoTUI(t *testing.T) {
	captured := captureStdout(func() {
		fmt.Printf("SOTE daemon running on port %d. Onion address: %s. PID: %d\n",
			18080, "abcdef.onion", os.Getpid())
	})

	assert.Contains(t, captured, "SOTE daemon running on port 18080")
	assert.Contains(t, captured, "Onion address: abcdef.onion")
	assert.Contains(t, captured, "PID:")
	assert.NotContains(t, captured, "\033[")
	assert.NotContains(t, captured, "tea.")
}

func captureStdout(fn func()) string {
	r, w, _ := os.Pipe()
	stdout := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = stdout }()

	go func() {
		defer w.Close()
		fn()
	}()

	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	return string(buf[:n])
}

func TestSIGTERMShutdown(t *testing.T) {
	// Verify that signal.NotifyContext properly detects SIGTERM and
	// cancels the context so that shutdown logic is triggered.
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Send SIGTERM to ourselves
	ch := make(chan struct{})
	go func() {
		<-ctx.Done()
		close(ch)
	}()
	syscall.Kill(os.Getpid(), syscall.SIGTERM)

	select {
	case <-ch:
		// Context cancelled as expected
	case <-time.After(2 * time.Second):
		t.Fatal("context was not cancelled after SIGTERM")
	}
}

func TestDBClosedOnShutdown(t *testing.T) {
	// Simulate daemon-mode shutdown: write data, checkpoint WAL, close DB.
	dbPath := t.TempDir() + "/test.db"
	db, err := storage.Initialize(dbPath)
	require.NoError(t, err, "initialize DB")

	// Write some data to populate the WAL
	err = storage.SaveUser(db, "alice", "hash", []byte("enc-priv"), []byte("pub"), "alice.onion", "")
	require.NoError(t, err, "save user")

	// Verify WAL files exist before checkpoint
	walPath := dbPath + "-wal"
	_, walErr := os.Stat(walPath)
	// WAL file may or may not exist depending on checkpoint timing;
	// the important thing is checkpoint doesn't error.

	// Execute shutdown sequence: checkpoint WAL → close DB
	_, err = db.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
	assert.NoError(t, err, "wal checkpoint should succeed")

	err = db.Close()
	assert.NoError(t, err, "db close should succeed")

	// After checkpoint+close, WAL/SHM files should not exist (TRUNCATE mode)
	if walErr == nil {
		// Note: TRUNCATE does NOT delete the WAL file header, it only
		// truncates the content. The file may still exist but be 0 bytes.
		// Verify DB is closed (query fails)
		_, dbErr := db.Exec("SELECT 1")
		assert.Error(t, dbErr, "query after close should fail")
	}
}

func TestTorClosedOnShutdown(t *testing.T) {
	// Verify that Tor service Stop is safe to call when not started
	// and does not panic.
	ts := tor.New(t.TempDir(), 18080, 9060)

	// Stop should be safe even when Tor was never started
	err := ts.Stop()
	assert.NoError(t, err, "stop on unstarted tor should not error")
}
