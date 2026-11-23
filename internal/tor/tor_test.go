package tor

import (
	"context"
	"net"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func torInstalled() bool {
	_, err := exec.LookPath("tor")
	return err == nil
}

// torAvailable reports whether tests that launch a real Tor process may run.
// Those tests connect to the live Tor network and wait for an onion service to
// be published, which takes minutes and needs Internet access, so they only
// run when SOTE_TOR_TESTS=1 is set explicitly.
func torAvailable() bool {
	return os.Getenv("SOTE_TOR_TESTS") == "1" && torInstalled()
}

func TestStartStop(t *testing.T) {
	if !torAvailable() {
		t.Skip("set SOTE_TOR_TESTS=1 to run tests that start a real Tor process")
	}

	ts := New(t.TempDir(), 18080, 9060)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	err := ts.Start(ctx)
	require.NoError(t, err)

	addr := ts.GetOnionAddress()
	assert.NotEmpty(t, addr)
	assert.Contains(t, addr, ".onion")

	err = ts.Stop()
	assert.NoError(t, err)

	assert.Empty(t, ts.GetOnionAddress())
}

func TestGetOnionAddress(t *testing.T) {
	if !torAvailable() {
		t.Skip("set SOTE_TOR_TESTS=1 to run tests that start a real Tor process")
	}

	ts := New(t.TempDir(), 18080, 9062)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	err := ts.Start(ctx)
	require.NoError(t, err)
	defer ts.Stop()

	addr := ts.GetOnionAddress()
	require.NotEmpty(t, addr)
	assert.Contains(t, addr, ".onion")

	addr2 := ts.GetOnionAddress()
	assert.Equal(t, addr, addr2)
}

func TestContextCancellation(t *testing.T) {
	if !torAvailable() {
		t.Skip("set SOTE_TOR_TESTS=1 to run tests that start a real Tor process")
	}

	ts := New(t.TempDir(), 18080, 9064)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := ts.Start(ctx)
	assert.Error(t, err)

	ts.Stop()
}

func TestTorNotFound(t *testing.T) {
	if torInstalled() {
		t.Skip("tor is installed, skipping 'not found' test")
	}

	ts := New(t.TempDir(), 18080, 9060)
	err := ts.Start(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Tor not found")
}

func TestDynamicPortAllocation(t *testing.T) {
	if !torAvailable() {
		t.Skip("set SOTE_TOR_TESTS=1 to run tests that start a real Tor process")
	}

	occupiedPort := 9070
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	// We need a predictable port, so listen on the exact port.
	ln.Close()

	ln, err = net.Listen("tcp", "127.0.0.1:9070")
	require.NoError(t, err)
	defer ln.Close()

	ts := New(t.TempDir(), 28080, occupiedPort)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	err = ts.Start(ctx)
	require.NoError(t, err)
	defer ts.Stop()

	addr := ts.GetOnionAddress()
	assert.NotEmpty(t, addr)
	assert.Contains(t, addr, ".onion")

	ln.Close()
}

func TestDoubleStart(t *testing.T) {
	if !torAvailable() {
		t.Skip("set SOTE_TOR_TESTS=1 to run tests that start a real Tor process")
	}

	ts := New(t.TempDir(), 18080, 9075)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	err := ts.Start(ctx)
	require.NoError(t, err)
	defer ts.Stop()

	err = ts.Start(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already running")
}

func TestStopWithoutStart(t *testing.T) {
	ts := New(t.TempDir(), 18080, 9080)
	err := ts.Stop()
	assert.NoError(t, err)
}

func TestGetOnionAddressWithoutStart(t *testing.T) {
	ts := New(t.TempDir(), 18080, 9090)
	assert.Empty(t, ts.GetOnionAddress())
}
