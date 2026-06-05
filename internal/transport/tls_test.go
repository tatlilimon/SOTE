package transport

import (
	"crypto/tls"
	"crypto/x509"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCAGeneration(t *testing.T) {
	certDir := filepath.Join(t.TempDir(), "certs")
	m := NewCertManager(certDir)

	err := m.GenerateCA()
	require.NoError(t, err)

	ca := m.CACert()
	require.NotNil(t, ca)
	assert.Equal(t, "SOTE Root CA", ca.Subject.CommonName)
	assert.Equal(t, ca.Subject.CommonName, ca.Issuer.CommonName, "CA must be self-signed")

	expectedExpiry := time.Now().Add(caValidity)
	assert.WithinDuration(t, expectedExpiry, ca.NotAfter, 48*time.Hour, "CA must have 3650-day validity")

	assert.True(t, ca.IsCA, "CA must have IsCA=true")
	assert.Equal(t, x509.KeyUsageCertSign|x509.KeyUsageCRLSign, ca.KeyUsage,
		"CA must have CertSign+CRLSign key usage")

	caCertPath := filepath.Join(certDir, caCertFile)
	caKeyPath := filepath.Join(certDir, caKeyFile)
	assert.FileExists(t, caCertPath)
	assert.FileExists(t, caKeyPath)

	info, err := os.Stat(caKeyPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0600), info.Mode().Perm())

	info, err = os.Stat(caCertPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0644), info.Mode().Perm())
}

func TestServerCertGeneration(t *testing.T) {
	certDir := filepath.Join(t.TempDir(), "certs")
	m := NewCertManager(certDir)

	err := m.GenerateCA()
	require.NoError(t, err)
	err = m.GenerateServerCert()
	require.NoError(t, err)

	sc := m.ServerCert()
	require.NotNil(t, sc)

	expectedExpiry := time.Now().Add(serverValidity)
	assert.WithinDuration(t, expectedExpiry, sc.NotAfter, 48*time.Hour,
		"server cert must have 365-day validity")
	assert.False(t, sc.IsCA, "server cert must not be a CA")

	roots := x509.NewCertPool()
	roots.AddCert(m.CACert())
	opts := x509.VerifyOptions{
		Roots:     roots,
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	_, err = sc.Verify(opts)
	assert.NoError(t, err, "server cert must be verifiable by CA")

	assert.Contains(t, sc.DNSNames, "localhost")

	var hasLoopback bool
	for _, ip := range sc.IPAddresses {
		if ip.Equal(net.ParseIP("127.0.0.1")) {
			hasLoopback = true
			break
		}
	}
	assert.True(t, hasLoopback, "server cert must include 127.0.0.1 IP SAN")

	serverCertPath := filepath.Join(certDir, serverCertFile)
	serverKeyPath := filepath.Join(certDir, serverKeyFile)
	assert.FileExists(t, serverCertPath)
	assert.FileExists(t, serverKeyPath)

	info, err := os.Stat(serverKeyPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0600), info.Mode().Perm())

	info, err = os.Stat(serverCertPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0644), info.Mode().Perm())
}

func TestFingerprint(t *testing.T) {
	certDir := filepath.Join(t.TempDir(), "certs")
	m := NewCertManager(certDir)

	err := m.GenerateCA()
	require.NoError(t, err)
	err = m.GenerateServerCert()
	require.NoError(t, err)

	fp := Fingerprint(m.ServerCert())
	assert.NotEmpty(t, fp)
	assert.Len(t, fp, 64, "SHA-256 fingerprint must be 64 hex chars")
	assert.Equal(t, fp, Fingerprint(m.ServerCert()), "fingerprint must be deterministic")
	assert.NotEqual(t, fp, Fingerprint(m.CACert()), "different certs must have different fingerprints")
}

func TestFingerprintVerification_TLSManager(t *testing.T) {
	certDir := filepath.Join(t.TempDir(), "certs")
	m := NewCertManager(certDir)

	err := m.GenerateCA()
	require.NoError(t, err)
	err = m.GenerateServerCert()
	require.NoError(t, err)

	fp := Fingerprint(m.ServerCert())

	clientConfig, err := ClientTLSConfigWithFingerprint(m.CACert(), fp)
	require.NoError(t, err)

	serverConfig, err := m.ServerTLSConfig()
	require.NoError(t, err)

	listener, err := tls.Listen("tcp", "127.0.0.1:0", serverConfig)
	require.NoError(t, err)
	defer listener.Close()

	errCh := make(chan error, 1)
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			errCh <- acceptErr
			return
		}
		buf := make([]byte, 1)
		_, _ = conn.Read(buf)
		conn.Close()
		errCh <- nil
	}()

	conn, err := tls.Dial("tcp", listener.Addr().String(), clientConfig)
	require.NoError(t, err, "connection with correct fingerprint must succeed")

	cs := conn.ConnectionState()
	require.GreaterOrEqual(t, len(cs.PeerCertificates), 1)
	assert.Equal(t, fp, Fingerprint(cs.PeerCertificates[0]),
		"connected peer fingerprint must match expected")
	conn.Close()

	require.NoError(t, <-errCh)
}

func TestFingerprintMismatch(t *testing.T) {
	certDir := filepath.Join(t.TempDir(), "certs")
	m := NewCertManager(certDir)

	err := m.GenerateCA()
	require.NoError(t, err)
	err = m.GenerateServerCert()
	require.NoError(t, err)

	wrongFP := "0000000000000000000000000000000000000000000000000000000000000000"

	clientConfig, err := ClientTLSConfigWithFingerprint(m.CACert(), wrongFP)
	require.NoError(t, err)

	serverConfig, err := m.ServerTLSConfig()
	require.NoError(t, err)

	listener, err := tls.Listen("tcp", "127.0.0.1:0", serverConfig)
	require.NoError(t, err)
	defer listener.Close()

	go func() {
		conn, _ := listener.Accept()
		if conn != nil {
			buf := make([]byte, 1)
			_, _ = conn.Read(buf)
			conn.Close()
		}
	}()

	_, err = tls.Dial("tcp", listener.Addr().String(), clientConfig)
	assert.Error(t, err, "connection with wrong fingerprint must be rejected")
	assert.Contains(t, err.Error(), "fingerprint", "error must mention fingerprint")
}

func TestLoadOrGenerate_Idempotent(t *testing.T) {
	certDir := filepath.Join(t.TempDir(), "certs")

	m1 := NewCertManager(certDir)
	err := m1.LoadOrGenerate()
	require.NoError(t, err)
	fp1 := Fingerprint(m1.ServerCert())

	m2 := NewCertManager(certDir)
	err = m2.LoadOrGenerate()
	require.NoError(t, err)
	fp2 := Fingerprint(m2.ServerCert())

	assert.Equal(t, fp1, fp2, "reloading certs must produce same fingerprint")
}

func TestCertDir(t *testing.T) {
	dir, err := CertDir()
	require.NoError(t, err)
	assert.Contains(t, dir, ".sote")
	assert.Contains(t, dir, "certs")
}

func TestNeedsRegeneration(t *testing.T) {
	certDir := filepath.Join(t.TempDir(), "certs")
	m := NewCertManager(certDir)

	err := m.GenerateCA()
	require.NoError(t, err)
	err = m.GenerateServerCert()
	require.NoError(t, err)

	assert.False(t, m.NeedsRegeneration(), "fresh cert must not need regeneration")

	m.serverCert.NotAfter = time.Now().Add(15 * 24 * time.Hour)
	assert.True(t, m.NeedsRegeneration(), "cert within 30 days of expiry must regenerate")

	m.serverCert.NotAfter = time.Now().Add(400 * 24 * time.Hour)
	assert.False(t, m.NeedsRegeneration(), "cert far from expiry must not regenerate")
}

func TestServerTLSConfig_NoInsecureSkipVerify(t *testing.T) {
	certDir := filepath.Join(t.TempDir(), "certs")
	m := NewCertManager(certDir)

	err := m.GenerateCA()
	require.NoError(t, err)
	err = m.GenerateServerCert()
	require.NoError(t, err)

	cfg, err := m.ServerTLSConfig()
	require.NoError(t, err)

	assert.False(t, cfg.InsecureSkipVerify, "S1: InsecureSkipVerify must NEVER be true")
	assert.NotNil(t, cfg.Certificates)
	assert.GreaterOrEqual(t, len(cfg.Certificates), 1)
}

func TestClientTLSConfig_NoInsecureSkipVerify(t *testing.T) {
	certDir := filepath.Join(t.TempDir(), "certs")
	m := NewCertManager(certDir)

	err := m.GenerateCA()
	require.NoError(t, err)

	cfg, err := ClientTLSConfigWithFingerprint(m.CACert(),
		"aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899")
	require.NoError(t, err)

	assert.False(t, cfg.InsecureSkipVerify, "S1: InsecureSkipVerify must NEVER be true")
	assert.NotNil(t, cfg.RootCAs)
	assert.NotNil(t, cfg.VerifyConnection)
}
