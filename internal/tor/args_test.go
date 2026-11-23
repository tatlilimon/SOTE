package tor

import (
	"crypto/ed25519"
	"crypto/rand"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"sote/internal/peer"
)

func TestTorArgsWithoutBridges(t *testing.T) {
	args := torArgs(9060, false, []string{"obfs4 192.0.2.1:443 cert=x"}, nil)
	assert.Equal(t, []string{"--SocksPort", "127.0.0.1:9060"}, args, "bridges listed but switched off are not used")
}

func TestTorArgsWithBridges(t *testing.T) {
	args := torArgs(9060, true,
		[]string{"obfs4 192.0.2.1:443 0123456789ABCDEF0123456789ABCDEF01234567 cert=abc iat-mode=0", "192.0.2.2:9001"},
		[]string{"obfs4 exec /usr/bin/lyrebird"})
	assert.Equal(t, []string{
		"--SocksPort", "127.0.0.1:9060",
		"--UseBridges", "1",
		"--ClientTransportPlugin", "obfs4 exec /usr/bin/lyrebird",
		"--Bridge", "obfs4 192.0.2.1:443 0123456789ABCDEF0123456789ABCDEF01234567 cert=abc iat-mode=0",
		"--Bridge", "192.0.2.2:9001",
	}, args)
	for _, a := range args {
		assert.False(t, strings.Contains(a, "\n"), "each option is a separate argument")
	}
}

func TestOnionAddressForKeyIsStableAndValid(t *testing.T) {
	_, key, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	a := OnionAddressForKey(key)
	assert.Equal(t, a, OnionAddressForKey(key))
	assert.True(t, peer.ValidOnion(a), a)
}
