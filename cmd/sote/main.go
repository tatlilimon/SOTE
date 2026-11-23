package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	tea "charm.land/bubbletea/v2"

	"sote/internal/auth"
	"sote/internal/config"
	"sote/internal/contacts"
	"sote/internal/crypto"
	"sote/internal/gui"
	"sote/internal/integration"
	"sote/internal/logging"
	"sote/internal/messages"
	"sote/internal/storage"
	"sote/internal/tor"
	"sote/internal/transport"
	"sote/internal/tui"
)

func main() {
	useGUI := flag.Bool("gui", false, "launch with GUI instead of TUI")
	flag.Parse()

	home, err := os.UserHomeDir()
	if err != nil {
		fatal("cannot determine home directory: %v", err)
	}

	soteDir := filepath.Join(home, ".sote")
	// Create the directory owner-only before anything is written into it.
	if err := os.MkdirAll(soteDir, 0o700); err != nil {
		fatal("create %s: %v", soteDir, err)
	}
	configPath := filepath.Join(soteDir, "config.yaml")
	logPath := filepath.Join(soteDir, "sote.log")

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		if err := config.SaveDefaultConfig(configPath); err != nil {
			fatal("create default config: %v", err)
		}
	}

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		fatal("load config: %v", err)
	}

	logging.InitLogger(cfg.LogLevel, cfg.LogFormat, logPath)
	slog.Info("SOTE starting", "version", tui.Version)

	db, err := storage.Initialize(cfg.DBPath)
	if err != nil {
		fatal("database: %v", err)
	}

	onionKey, err := loadOrCreateOnionKey(filepath.Join(soteDir, "onion_v3.key"))
	if err != nil {
		fatal("onion service key: %v", err)
	}

	// The peer server listens on loopback only; Tor forwards the onion
	// service's port 443 to it.
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", cfg.ListenPort))
	if err != nil {
		fatal("listen on 127.0.0.1:%d (is another SOTE running? change listen_port in %s): %v", cfg.ListenPort, configPath, err)
	}

	cryptoSvc := crypto.New()
	authSvc := auth.New(db)
	contactSvc := contacts.NewService(db)
	msgSvc := messages.NewService(db, cryptoSvc)

	torSvc := tor.New(filepath.Join(soteDir, "tor-data"), cfg.ListenPort, cfg.TorSocksPort)
	torSvc.SetOnionKey(onionKey)
	torSvc.SetListener(ln)
	torSvc.SetBridges(cfg.UseBridges, cfg.Bridges, cfg.ClientTransportPlugins)

	node := integration.New(integration.Options{
		DB: db, Crypto: cryptoSvc, Contacts: contactSvc, Messages: msgSvc,
		Tor: torSvc, Listener: ln, OnionAddress: tor.OnionAddressForKey(onionKey), TLS: serverTLS(),
		DialClient: func(ctx context.Context) (*transport.Client, error) {
			d, err := torSvc.Dialer(ctx)
			if err != nil {
				return nil, err
			}
			return transport.NewClient(transport.WithProxyDialer(d)), nil
		},
	})

	deps := integration.Deps{Node: node, Config: cfg, ConfigPath: configPath, DB: db,
		Auth: authSvc, Crypto: cryptoSvc, Contacts: contactSvc, Messages: msgSvc, Tor: torSvc}

	if *useGUI {
		if err := gui.Run(deps); err != nil {
			fatal("%v", err)
		}
		return
	}
	runTUI(deps)
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

func serverTLS() *tls.Config {
	certDir, err := transport.CertDir()
	if err != nil {
		return nil
	}
	cm := transport.NewCertManager(certDir)
	if err := cm.LoadOrGenerate(); err != nil {
		slog.Warn("TLS cert generation failed, serving plain HTTP inside Tor", "error", err)
		return nil
	}
	tlsCfg, err := cm.ServerTLSConfig()
	if err != nil {
		return nil
	}
	slog.Info("TLS configured", "cert_dir", certDir)
	return tlsCfg
}

// loadOrCreateOnionKey keeps the onion address stable: the v3 address is
// derived from this key, so losing it means a new address and contacts who
// can no longer reach the user. Stored as PKCS#8 PEM, owner-only.
func loadOrCreateOnionKey(path string) (ed25519.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err == nil {
		block, _ := pem.Decode(data)
		if block == nil || block.Type != "PRIVATE KEY" {
			return nil, fmt.Errorf("%s: not a PEM private key", path)
		}
		key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		ed, ok := key.(ed25519.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("%s: not an ed25519 key", path)
		}
		return ed, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	if err := pem.Encode(f, &pem.Block{Type: "PRIVATE KEY", Bytes: der}); err != nil {
		f.Close()
		return nil, err
	}
	return key, f.Close()
}

func runTUI(d integration.Deps) {
	model := tui.NewModel(d.Config, d.DB, d.Auth, d.Crypto, d.Contacts, d.Messages, d.Tor, d.Node.Server(), nil).
		WithDeps(d)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	p := tea.NewProgram(model, tea.WithContext(ctx))
	d.Node.OnEvent(func(e integration.Event) { p.Send(tui.NodeEventMsg{Event: e}) })
	_, err := p.Run()
	d.Node.Stop()
	if _, cerr := d.DB.Exec("PRAGMA wal_checkpoint(TRUNCATE)"); cerr != nil {
		slog.Error("wal checkpoint", "error", cerr)
	}
	d.DB.Close()
	if err != nil && !errors.Is(err, tea.ErrProgramKilled) && !errors.Is(err, context.Canceled) {
		slog.Error("tui error", "error", err)
		os.Exit(1)
	}
}
