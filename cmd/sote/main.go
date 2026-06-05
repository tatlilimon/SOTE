package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	tea "charm.land/bubbletea/v2"

	"sote/internal/auth"
	"sote/internal/config"
	"sote/internal/contacts"
	"sote/internal/crypto"
	"sote/internal/logging"
	"sote/internal/messages"
	"sote/internal/storage"
	"sote/internal/tor"
	"sote/internal/transport"
	"sote/internal/tui"
)

func main() {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot determine home directory: %v\n", err)
		os.Exit(1)
	}

	soteDir := filepath.Join(home, ".sote")
	configPath := filepath.Join(soteDir, "config.yaml")
	logPath := filepath.Join(soteDir, "sote.log")

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		if err := config.SaveDefaultConfig(configPath); err != nil {
			fmt.Fprintf(os.Stderr, "create default config: %v\n", err)
			os.Exit(1)
		}
	}

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		os.Exit(1)
	}

	logging.InitLogger(cfg.LogLevel, cfg.LogFormat, logPath)
	slog.Info("SOTE starting", "version", tui.Version)

	os.MkdirAll(soteDir, 0700)

	db, err := storage.Initialize(cfg.DBPath)
	if err != nil {
		slog.Error("database init", "error", err)
		os.Exit(1)
	}

	cryptoSvc := crypto.New()
	authSvc := auth.New(db)
	contactSvc := contacts.NewService(db)
	msgSvc := messages.NewService(db, cryptoSvc)

	torDataDir := filepath.Join(soteDir, "tor-data")
	torSvc := tor.New(torDataDir, cfg.ListenPort, cfg.TorSocksPort)

	addr := fmt.Sprintf(":%d", cfg.ListenPort)
	srv := transport.NewServer(authSvc, contactSvc, msgSvc, torSvc, db, addr)

	certDir, err := transport.CertDir()
	if err == nil {
		cm := transport.NewCertManager(certDir)
		if err := cm.LoadOrGenerate(); err != nil {
			slog.Warn("TLS cert generation, running without TLS", "error", err)
		} else if tlsCfg, err := cm.ServerTLSConfig(); err == nil {
			srv.WithTLS(tlsCfg)
			slog.Info("TLS configured", "cert_dir", certDir)
		}
	}

	client := transport.NewClient()

	runTUI(cfg, db, authSvc, cryptoSvc, contactSvc, msgSvc, torSvc, srv, client)
}

func runTUI(cfg *config.Config, db *sql.DB,
	authSvc *auth.AuthService, cryptoSvc *crypto.Service,
	contactSvc *contacts.Service, msgSvc *messages.Service,
	torSvc *tor.TorService, srv *transport.Server, client *transport.Client,
) {
	model := tui.NewModel(cfg, db, authSvc, cryptoSvc, contactSvc, msgSvc, torSvc, srv, client)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	opts := []tea.ProgramOption{
		tea.WithContext(ctx),
	}

	p := tea.NewProgram(model, opts...)
	if _, err := p.Run(); err != nil {
		slog.Error("tui error", "error", err)
		os.Exit(1)
	}
}
