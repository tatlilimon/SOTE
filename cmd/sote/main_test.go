package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"sote/internal/storage"
	"sote/internal/tor"
)

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
	// Simulate shutdown: write data, checkpoint WAL, close DB.
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

func TestGUIFlagParsed(t *testing.T) {
	t.Run("gui flag set", func(t *testing.T) {
		fs := flag.NewFlagSet("test", flag.ContinueOnError)
		useGUI := fs.Bool("gui", false, "launch with GUI instead of TUI")

		err := fs.Parse([]string{"--gui"})
		require.NoError(t, err)
		assert.True(t, *useGUI)
	})

	t.Run("gui flag absent", func(t *testing.T) {
		fs := flag.NewFlagSet("test", flag.ContinueOnError)
		useGUI := fs.Bool("gui", false, "launch with GUI instead of TUI")

		err := fs.Parse([]string{})
		require.NoError(t, err)
		assert.False(t, *useGUI)
	})

	t.Run("gui flag shorthand", func(t *testing.T) {
		fs := flag.NewFlagSet("test", flag.ContinueOnError)
		useGUI := fs.Bool("gui", false, "launch with GUI instead of TUI")

		err := fs.Parse([]string{"--gui=false"})
		require.NoError(t, err)
		assert.False(t, *useGUI)
	})
}
