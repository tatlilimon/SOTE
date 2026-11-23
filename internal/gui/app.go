//go:build !nogui

package gui

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"

	"sote/internal/auth"
	"sote/internal/config"
	"sote/internal/contacts"
	"sote/internal/crypto"
	"sote/internal/domain"
	"sote/internal/messages"
	"sote/internal/storage"
	"sote/internal/tor"
	"sote/internal/transport"
)

type passwordChanger struct {
	db     *sql.DB
	crypto *crypto.Service
}

// ChangePassword delegates to the auth service, which updates the password
// hash, the encrypted private key and stored message copies in one
// transaction. (It used to INSERT a second user row, which always failed.)
func (pc *passwordChanger) ChangePassword(username, currentPassword, newPassword string) error {
	return auth.New(pc.db).ChangePassword(context.Background(), username, currentPassword, newPassword)
}

func Run(cfg *config.Config, db *sql.DB,
	authSvc *auth.AuthService, cryptoSvc *crypto.Service,
	contactSvc *contacts.Service, msgSvc *messages.Service,
	torSvc *tor.TorService, srv *transport.Server, client *transport.Client,
) error {
	a := app.NewWithID("sote.gui")
	a.Settings().SetTheme(NewSOTETheme())
	w := createWindow(a)

	trayMgr := NewTrayManager(a, w)

	loginView := NewLoginView(w, authSvc)
	tabs := container.NewAppTabs()
	tabs.Hide()

	contentStack := container.NewMax(loginView.Content(), tabs)
	w.SetContent(contentStack)

	doneCh := make(chan struct{})

	loginView.OnLoginSuccess = func(user *domain.User) {
		chatView := NewChatView(msgSvc, user.Username, user.RawPassword, nil)
		contactsPage := NewContactsPage(contactSvc, user.Username, w)
		settingsView := NewSettings(w, user, &passwordChanger{db: db, crypto: cryptoSvc})

		chatView.OnIncomingMessage = func(sender string) {
			trayMgr.NotifyIncoming(sender)
		}

		tabs.Append(container.NewTabItem("Chat", chatView.Container()))
		tabs.Append(container.NewTabItem("Contacts", contactsPage.Content()))
		tabs.Append(container.NewTabItem("Settings", settingsView.Content()))
		tabs.SelectTabIndex(0)

		loginView.Content().Hide()
		tabs.Show()
		contentStack.Refresh()

		contactsPage.LoadContacts()
		trayMgr.SetConnected(true)

		go pollMessages(chatView, contactsPage, 30*time.Second, doneCh)
	}

	w.SetOnClosed(func() {
		close(doneCh)

		slog.Info("stopping Tor")
		if err := torSvc.Stop(); err != nil {
			slog.Error("tor stop", "error", err)
		}
		slog.Info("stopping transport server")
		if err := srv.Stop(); err != nil {
			slog.Error("server stop", "error", err)
		}
		slog.Info("checkpointing WAL")
		if _, err := db.Exec("PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
			slog.Error("wal checkpoint", "error", err)
		}
		slog.Info("closing database")
		if err := db.Close(); err != nil {
			slog.Error("db close", "error", err)
		}
		slog.Info("SOTE GUI shutdown complete")
	})

	_ = client
	_ = cfg

	w.ShowAndRun()
	return nil
}

// pollMessages periodically refreshes message list and contacts for the
// current chat view. It runs until doneCh is closed.
func pollMessages(cv *ChatView, cp *ContactsPage, interval time.Duration, doneCh <-chan struct{}) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-doneCh:
			return
		case <-ticker.C:
			fyne.Do(func() {
				if cv.SelectedContact() != "" {
					cv.loadMessages()
				}
				cp.LoadContacts()
			})
		}
	}
}

func createWindow(a fyne.App) fyne.Window {
	w := a.NewWindow("SOTE — Decentralized Messenger")
	w.Resize(fyne.NewSize(1024, 768))
	w.SetMaster()
	return w
}
