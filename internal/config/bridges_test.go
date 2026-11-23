package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	obfs4Line = "obfs4 192.0.2.10:443 0123456789ABCDEF0123456789ABCDEF01234567 cert=AbCd+/xyz iat-mode=0"
	plugin    = "obfs4 exec /usr/bin/lyrebird"
)

func TestNormalizeBridgeLine(t *testing.T) {
	cases := map[string]string{
		obfs4Line:                     obfs4Line,
		"Bridge   " + obfs4Line + " ": obfs4Line,
		"192.0.2.1:9001":              "192.0.2.1:9001",
		"192.0.2.1:9001 0123456789ABCDEF0123456789ABCDEF01234567":                                                     "192.0.2.1:9001 0123456789ABCDEF0123456789ABCDEF01234567",
		"webtunnel [2001:db8::1]:443 0123456789ABCDEF0123456789ABCDEF01234567 url=https://example.org/path ver=0.0.1": "webtunnel [2001:db8::1]:443 0123456789ABCDEF0123456789ABCDEF01234567 url=https://example.org/path ver=0.0.1",
	}
	for in, want := range cases {
		got, err := NormalizeBridgeLine(in)
		require.NoError(t, err, in)
		assert.Equal(t, want, got)
	}
	for _, bad := range []string{
		"", "Bridge", "obfs4", "obfs4 example.com:443", "obfs4 192.0.2.1:99999",
		"obfs4 192.0.2.1:443 nothex", "obfs4 192.0.2.1:443 cert=x\nSocksPort 0.0.0.0:9050",
		"ob fs4! 192.0.2.1:443", "192.0.2.1:443 junk",
	} {
		_, err := NormalizeBridgeLine(bad)
		assert.Error(t, err, "%q", bad)
	}
}

func TestNormalizePluginLine(t *testing.T) {
	line, ts, err := NormalizePluginLine("ClientTransportPlugin obfs4,webtunnel exec /usr/bin/lyrebird")
	require.NoError(t, err)
	assert.Equal(t, "obfs4,webtunnel exec /usr/bin/lyrebird", line)
	assert.Equal(t, []string{"obfs4", "webtunnel"}, ts)

	for _, bad := range []string{"obfs4 exec lyrebird", "obfs4 socks5 127.0.0.1:1", "obfs4", "ob$ exec /bin/x"} {
		_, _, err := NormalizePluginLine(bad)
		assert.Error(t, err, bad)
	}
}

func TestValidateBridges(t *testing.T) {
	assert.ErrorIs(t, ValidateBridges(true, nil, nil), ErrBridgesRequired)
	assert.NoError(t, ValidateBridges(false, nil, nil))
	assert.NoError(t, ValidateBridges(true, []string{"192.0.2.1:9001"}, nil), "plain bridges need no plugin")
	assert.NoError(t, ValidateBridges(true, []string{obfs4Line}, []string{plugin}))

	err := ValidateBridges(true, []string{obfs4Line}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "obfs4")
}

func TestParseBridgeTextRoundTrip(t *testing.T) {
	text := "# from bridges.torproject.org\n\nBridge " + obfs4Line + "\nClientTransportPlugin " + plugin + "\n"
	bridges, plugins, err := ParseBridgeText(text)
	require.NoError(t, err)
	assert.Equal(t, []string{obfs4Line}, bridges)
	assert.Equal(t, []string{plugin}, plugins)

	b2, p2, err := ParseBridgeText(BridgeText(bridges, plugins))
	require.NoError(t, err)
	assert.Equal(t, bridges, b2)
	assert.Equal(t, plugins, p2)
}

func TestSaveConfigIsOwnerOnlyAndValidated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "config.yaml")
	cfg := DefaultConfig()
	cfg.UseBridges, cfg.Bridges, cfg.ClientTransportPlugins = true, []string{obfs4Line}, []string{plugin}
	require.NoError(t, SaveConfig(path, cfg))

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	dirInfo, err := os.Stat(filepath.Dir(path))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), dirInfo.Mode().Perm())

	loaded, err := LoadConfig(path)
	require.NoError(t, err)
	assert.True(t, loaded.UseBridges)
	assert.Equal(t, cfg.Bridges, loaded.Bridges)

	bad := DefaultConfig()
	bad.UseBridges = true
	assert.Error(t, SaveConfig(path, bad), "invalid settings are not written")
	again, err := LoadConfig(path)
	require.NoError(t, err)
	assert.Equal(t, cfg.Bridges, again.Bridges, "the previous file is kept")
}

func TestLoadConfigRejectsBadBridges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte("use_bridges: true\nbridges:\n  - \"obfs4 192.0.2.1:443 cert=x\"\n"), 0o600))
	_, err := LoadConfig(path)
	assert.Error(t, err, "obfs4 without a transport plugin")
}
