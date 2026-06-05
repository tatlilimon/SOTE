package transport

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

const (
	caCertFile     = "ca.crt"
	caKeyFile      = "ca.key"
	serverCertFile = "server.crt"
	serverKeyFile  = "server.key"

	caValidity       = 3650 * 24 * time.Hour
	serverValidity   = 365 * 24 * time.Hour
	regenThreshold   = 30 * 24 * time.Hour
	serialNumberBits = 128
)

var errFingerprintMismatch = errors.New("fingerprint mismatch")

type CertManager struct {
	certDir    string
	ca         *x509.Certificate
	caKey      ed25519.PrivateKey
	serverCert *x509.Certificate
	serverKey  ed25519.PrivateKey
}

func NewCertManager(certDir string) *CertManager {
	return &CertManager{certDir: certDir}
}

func CertDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("user home directory: %w", err)
	}
	return filepath.Join(home, ".sote", "certs"), nil
}

func (m *CertManager) GenerateCA() error {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return fmt.Errorf("generate CA key: %w", err)
	}
	serial, err := randomSerial()
	if err != nil {
		return err
	}
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   "SOTE Root CA",
			Organization: []string{"SOTE"},
		},
		NotBefore:             now,
		NotAfter:              now.Add(caValidity),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            0,
	}
	certDER, err := x509.CreateCertificate(rand.Reader, template, template, pub, priv)
	if err != nil {
		return fmt.Errorf("create CA certificate: %w", err)
	}
	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		return fmt.Errorf("parse CA certificate: %w", err)
	}
	if err := m.makeCertDir(); err != nil {
		return err
	}
	if err := writePEMCert(filepath.Join(m.certDir, caCertFile), certDER, 0644); err != nil {
		return fmt.Errorf("save CA cert: %w", err)
	}
	if err := writePEMKey(filepath.Join(m.certDir, caKeyFile), priv, 0600); err != nil {
		return fmt.Errorf("save CA key: %w", err)
	}
	m.ca = cert
	m.caKey = priv
	return nil
}

func (m *CertManager) GenerateServerCert() error {
	if m.ca == nil || m.caKey == nil {
		return errors.New("CA not loaded: call GenerateCA or LoadOrGenerate first")
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return fmt.Errorf("generate server key: %w", err)
	}
	serial, err := randomSerial()
	if err != nil {
		return err
	}
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   "localhost",
			Organization: []string{"SOTE"},
		},
		NotBefore: now,
		NotAfter:  now.Add(serverValidity),
		KeyUsage:  x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{
			x509.ExtKeyUsageServerAuth,
		},
		DNSNames:    []string{"localhost"},
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
	}
	certDER, err := x509.CreateCertificate(rand.Reader, template, m.ca, pub, m.caKey)
	if err != nil {
		return fmt.Errorf("create server certificate: %w", err)
	}
	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		return fmt.Errorf("parse server certificate: %w", err)
	}
	if err := writePEMCert(filepath.Join(m.certDir, serverCertFile), certDER, 0644); err != nil {
		return fmt.Errorf("save server cert: %w", err)
	}
	if err := writePEMKey(filepath.Join(m.certDir, serverKeyFile), priv, 0600); err != nil {
		return fmt.Errorf("save server key: %w", err)
	}
	m.serverCert = cert
	m.serverKey = priv
	return nil
}

func (m *CertManager) LoadOrGenerate() error {
	caPath := filepath.Join(m.certDir, caCertFile)
	caKeyPath := filepath.Join(m.certDir, caKeyFile)
	serverPath := filepath.Join(m.certDir, serverCertFile)
	serverKeyPath := filepath.Join(m.certDir, serverKeyFile)
	caExists := fileExists(caPath) && fileExists(caKeyPath)
	serverExists := fileExists(serverPath) && fileExists(serverKeyPath)
	if caExists {
		ca, caKey, err := loadCertAndKey(caPath, caKeyPath)
		if err != nil {
			return fmt.Errorf("load CA: %w", err)
		}
		m.ca = ca
		m.caKey = caKey
	}
	if serverExists {
		cert, key, err := loadCertAndKey(serverPath, serverKeyPath)
		if err != nil {
			return fmt.Errorf("load server cert: %w", err)
		}
		m.serverCert = cert
		m.serverKey = key
	}
	if !caExists {
		if err := m.GenerateCA(); err != nil {
			return err
		}
	}
	if !serverExists || m.NeedsRegeneration() {
		if err := m.GenerateServerCert(); err != nil {
			return err
		}
	}
	return nil
}

func (m *CertManager) ServerTLSConfig() (*tls.Config, error) {
	if m.serverCert == nil || m.serverKey == nil {
		return nil, errors.New("server cert not loaded: call LoadOrGenerate first")
	}
	cert := tls.Certificate{
		Certificate: [][]byte{m.serverCert.Raw},
		PrivateKey:  m.serverKey,
	}
	return &tls.Config{
		Certificates:       []tls.Certificate{cert},
		MinVersion:         tls.VersionTLS13,
		InsecureSkipVerify: false,
	}, nil
}

func ClientTLSConfigWithFingerprint(peerCACert *x509.Certificate, expectedFP string) (*tls.Config, error) {
	if peerCACert == nil {
		return nil, errors.New("peer CA certificate is required")
	}
	if len(expectedFP) != 64 {
		return nil, errors.New("expected fingerprint must be 64 hex characters (SHA-256)")
	}
	pool := x509.NewCertPool()
	pool.AddCert(peerCACert)
	return &tls.Config{
		RootCAs:            pool,
		MinVersion:         tls.VersionTLS13,
		InsecureSkipVerify: false,
		VerifyConnection:   verifyFingerprint(expectedFP),
	}, nil
}

func Fingerprint(cert *x509.Certificate) string {
	if cert == nil {
		return ""
	}
	hash := sha256.Sum256(cert.Raw)
	return hex.EncodeToString(hash[:])
}

func (m *CertManager) CACert() *x509.Certificate { return m.ca }

func (m *CertManager) ServerCert() *x509.Certificate { return m.serverCert }

func (m *CertManager) NeedsRegeneration() bool {
	if m.serverCert == nil {
		return true
	}
	return time.Now().Add(regenThreshold).After(m.serverCert.NotAfter)
}

func verifyFingerprint(expectedFP string) func(tls.ConnectionState) error {
	return func(cs tls.ConnectionState) error {
		if len(cs.PeerCertificates) == 0 {
			return errors.New("no peer certificates presented")
		}
		actualFP := Fingerprint(cs.PeerCertificates[0])
		if actualFP != expectedFP {
			return fmt.Errorf("%w: expected %s, got %s", errFingerprintMismatch, expectedFP, actualFP)
		}
		return nil
	}
}

func (m *CertManager) makeCertDir() error {
	return os.MkdirAll(m.certDir, 0700)
}

func randomSerial() (*big.Int, error) {
	limit := new(big.Int).Lsh(big.NewInt(1), serialNumberBits)
	serial, err := rand.Int(rand.Reader, limit)
	if err != nil {
		return nil, fmt.Errorf("generate serial number: %w", err)
	}
	return serial, nil
}

func writePEMCert(path string, der []byte, perm os.FileMode) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	defer f.Close()
	return pem.Encode(f, &pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func writePEMKey(path string, key ed25519.PrivateKey, perm os.FileMode) error {
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return fmt.Errorf("marshal private key: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	defer f.Close()
	return pem.Encode(f, &pem.Block{Type: "PRIVATE KEY", Bytes: der})
}

func loadCertAndKey(certPath, keyPath string) (*x509.Certificate, ed25519.PrivateKey, error) {
	cert, err := loadCert(certPath)
	if err != nil {
		return nil, nil, err
	}
	key, err := loadED25519Key(keyPath)
	if err != nil {
		return nil, nil, err
	}
	return cert, key, nil
}

func loadCert(path string) (*x509.Certificate, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read cert file: %w", err)
	}
	block, _ := pem.Decode(data)
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, errors.New("invalid PEM: no CERTIFICATE block found")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse certificate: %w", err)
	}
	return cert, nil
}

func loadED25519Key(path string) (ed25519.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read key file: %w", err)
	}
	block, _ := pem.Decode(data)
	if block == nil || block.Type != "PRIVATE KEY" {
		return nil, errors.New("invalid PEM: no PRIVATE KEY block found")
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse private key: %w", err)
	}
	edKey, ok := key.(ed25519.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("unexpected key type: %T", key)
	}
	return edKey, nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
