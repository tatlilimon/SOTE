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
	"fyne.io/fyne/v2/dialog"

	"sote/internal/auth"
	"sote/internal/config"
	"sote/internal/crypto"
	"sote/internal/domain"
	"sote/internal/integration"
	"sote/internal/storage"
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

// Run shows the desktop interface until the window is closed. It starts the
// node (Tor, peer server, delivery) in the background; the GUI used to show
// "connected" without ever starting Tor.
func Run(d integration.Deps) error {
	a := app.NewWithID("sote.gui")
	a.Settings().SetTheme(NewSOTETheme())
	w := createWindow(a)

	trayMgr := NewTrayManager(a, w)

	loginView := NewLoginView(w, d.Auth)
	tabs := container.NewAppTabs()
	tabs.Hide()

	contentStack := container.NewMax(loginView.Content(), tabs)
	w.SetContent(contentStack)

	doneCh := make(chan struct{})
	node := d.Node

	var (
		chatView     *ChatView
		contactsPage *ContactsPage
		settingsView *Settings
		torOnline    bool
	)

	node.OnEvent(func(e integration.Event) {
		fyne.Do(func() {
			switch e.Kind {
			case integration.EventTorReady:
				torOnline = true
				trayMgr.SetConnected(true)
				if settingsView != nil {
					settingsView.SetTorStatus(true, d.Tor.SocksPort(), "")
				}
			case integration.EventTorFailed:
				torOnline = false
				trayMgr.SetConnected(false)
				if settingsView != nil {
					settingsView.SetTorStatus(false, 0, "")
				}
				dialog.ShowError(fmt.Errorf("Tor: %s", e.Detail), w)
			case integration.EventMessageReceived:
				trayMgr.NotifyIncoming(e.Peer)
				if chatView != nil && chatView.SelectedContact() == e.Peer {
					chatView.loadMessages()
				}
			case integration.EventDelivery:
				if chatView != nil && chatView.SelectedContact() == e.Peer {
					chatView.loadMessages()
				}
			case integration.EventContactRequest:
				if contactsPage != nil {
					contactsPage.LoadContacts()
				}
			}
		})
	})
	node.Start(context.Background())

	loginView.OnLoginSuccess = func(user *domain.User) {
		if err := node.Login(user); err != nil {
			dialog.ShowError(fmt.Errorf("could not unlock your key: %w", err), w)
			return
		}
		list, _ := storage.GetAllContacts(d.DB, user.Username)
		chatView = NewChatView(d.Messages, user.Username, user.RawPassword, list)
		contactsPage = NewContactsPage(d.Contacts, user.Username, w)
		contactsPage.SetNode(node)
		contactsPage.OnContactsChanged = chatView.SetContacts
		settingsView = NewSettings(w, user, &passwordChanger{db: d.DB, crypto: d.Crypto})
		settingsView.SetTorStatus(torOnline, d.Tor.SocksPort(), "")
		settingsView.SetBridges(d.Config.UseBridges,
			config.BridgeText(d.Config.Bridges, d.Config.ClientTransportPlugins), d.SaveBridges)

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
		trayMgr.SetConnected(torOnline)

		go pollMessages(chatView, contactsPage, 30*time.Second, doneCh)
	}

	w.SetOnClosed(func() {
		close(doneCh)

		slog.Info("stopping node (delivery, peer server, Tor)")
		if err := node.Stop(); err != nil {
			slog.Error("node stop", "error", err)
		}
		slog.Info("checkpointing WAL")
		if _, err := d.DB.Exec("PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
			slog.Error("wal checkpoint", "error", err)
		}
		slog.Info("closing database")
		if err := d.DB.Close(); err != nil {
			slog.Error("db close", "error", err)
		}
		slog.Info("SOTE GUI shutdown complete")
	})

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
