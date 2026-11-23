//go:build !nogui

package gui

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"sote/internal/auth"
	"sote/internal/crypto"
	"sote/internal/domain"
	"sote/internal/messages"
	"sote/internal/storage"
)

func setupIntegrationDB(t *testing.T) (authSvc *auth.AuthService, msgSvc *messages.Service, contactList []storage.Contact, username, rawPassword string) {
	t.Helper()

	db, err := storage.Initialize(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	cs := crypto.New()
	authSvc = auth.New(db)
	msgSvc = messages.NewService(db, cs)
	msgSvc.SetLocalOnion(func() string { return "local.onion" })

	user, err := authSvc.Register(context.Background(), "alice", "password123")
	require.NoError(t, err)
	require.NotNil(t, user)

	priv, pub, err := cs.GenerateKeyPair()
	require.NoError(t, err)

	hashed, err := cs.HashPassword("bobpass")
	require.NoError(t, err)

	aesPriv, err := cs.EncryptAES(priv, "bobpass")
	require.NoError(t, err)

	err = storage.SaveUser(db, "bob", hashed, aesPriv, pub, "bob.onion", "/tmp/bob.torrc")
	require.NoError(t, err)

	err = storage.SaveContact(db, "alice", "bob", "bob.onion", pub)
	require.NoError(t, err)

	alicePub, err := storage.GetUser(db, "alice")
	require.NoError(t, err)
	err = storage.SaveContact(db, "bob", "alice", "alice.onion", alicePub.PublicKey)
	require.NoError(t, err)

	contactList, err = storage.GetAllContacts(db, "alice")
	require.NoError(t, err)

	return authSvc, msgSvc, contactList, user.Username, user.RawPassword
}

func TestFullLoginFlow(t *testing.T) {
	a := test.NewApp()
	w := a.NewWindow("SOTE — Decentralized Messenger")

	authSvc, msgSvc, contactList, username, rawPassword := setupIntegrationDB(t)

	loginView := NewLoginView(w, authSvc)
	require.NotNil(t, loginView)

	tabs := container.NewAppTabs()
	tabs.Hide()

	contentStack := container.NewMax(loginView.Content(), tabs)
	w.SetContent(contentStack)

	var successCalled atomic.Bool
	var wg sync.WaitGroup
	wg.Add(1)

	loginView.OnLoginSuccess = func(user *domain.User) {
		_ = user
		successCalled.Store(true)
		loginView.Content().Hide()

		cv := NewChatView(msgSvc, username, rawPassword, contactList)
		cv.waitLoads()
		tabs.Append(container.NewTabItem("Chat", cv.Container()))
		tabs.Append(container.NewTabItem("Contacts", widget.NewLabel("Contacts Tab")))
		tabs.Append(container.NewTabItem("Settings", widget.NewLabel("Settings Tab")))
		tabs.SelectTabIndex(0)
		tabs.Show()
		contentStack.Refresh()
		wg.Done()
	}

	loginView.usernameEntry.SetText("alice")
	loginView.passwordEntry.SetText("password123")
	loginView.handleLogin()

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("login did not complete within timeout")
	}

	assert.True(t, successCalled.Load(), "OnLoginSuccess must be called after successful login")
	assert.Equal(t, 3, len(tabs.Items), "tabs must contain Chat, Contacts, and Settings")
	assert.Equal(t, "Chat", tabs.Items[0].Text)
	assert.Equal(t, "Contacts", tabs.Items[1].Text)
	assert.Equal(t, "Settings", tabs.Items[2].Text)
}

func TestTabSwitching(t *testing.T) {
	a := test.NewApp()
	w := a.NewWindow("SOTE")

	tabs := container.NewAppTabs(
		container.NewTabItem("Chat", widget.NewLabel("Chat content")),
		container.NewTabItem("Contacts", widget.NewLabel("Contacts content")),
		container.NewTabItem("Settings", widget.NewLabel("Settings content")),
	)
	w.SetContent(tabs)

	assert.Equal(t, 3, len(tabs.Items))
	assert.Equal(t, "Chat", tabs.Items[0].Text)

	tabs.SelectTabIndex(1)
	assert.Equal(t, 1, tabs.SelectedIndex())

	tabs.SelectTabIndex(2)
	assert.Equal(t, 2, tabs.SelectedIndex())

	tabs.SelectTabIndex(0)
	assert.Equal(t, 0, tabs.SelectedIndex())

	tabs.SelectTab(tabs.Items[2])
	assert.Equal(t, 2, tabs.SelectedIndex())

	assert.NotNil(t, tabs.Items[tabs.SelectedIndex()].Content)
	_ = a
}

func TestIncomingMessageNotification(t *testing.T) {
	_, msgSvc, contactList, username, rawPassword := setupIntegrationDB(t)

	cv := NewChatView(msgSvc, username, rawPassword, contactList)
	cv.waitLoads()
	require.NotNil(t, cv)
	require.Equal(t, "bob", cv.SelectedContact())

	var notifiedSender string
	var notifyCount atomic.Int32
	var wg2 sync.WaitGroup
	wg2.Add(1)

	cv.OnIncomingMessage = func(sender string) {
		notifiedSender = sender
		notifyCount.Add(1)
		wg2.Done()
	}

	err := msgSvc.SendMessage(context.Background(), "bob", username, "bobpass", "Hello from bob")
	require.NoError(t, err)

	cv.loadMessages()

	done2 := make(chan struct{})
	go func() {
		wg2.Wait()
		close(done2)
	}()

	select {
	case <-done2:
	case <-time.After(5 * time.Second):
		t.Fatal("message load did not complete within timeout")
	}

	msgs := cv.Messages()
	assert.NotEmpty(t, msgs, "messages must be loaded after polling")
	assert.Equal(t, "bob", notifiedSender, "OnIncomingMessage callback sender must be bob")
	assert.GreaterOrEqual(t, notifyCount.Load(), int32(1), "notification must fire at least once")
}

func TestLoginToTabsTransition(t *testing.T) {
	a := test.NewApp()
	w := a.NewWindow("SOTE Test")

	authSvc, msgSvc, contactList, username, rawPassword := setupIntegrationDB(t)

	loginView := NewLoginView(w, authSvc)
	tabs := container.NewAppTabs()
	tabs.Hide()

	contentStack := container.NewMax(loginView.Content(), tabs)
	w.SetContent(contentStack)

	var wg3 sync.WaitGroup
	wg3.Add(1)

	loginView.OnLoginSuccess = func(user *domain.User) {
		loginView.Content().Hide()

		cv := NewChatView(msgSvc, username, rawPassword, contactList)
		cv.waitLoads()
		cp := NewContactsPage(nil, username, w)
		sv := NewSettings(w, user, nil)

		tabs.Append(container.NewTabItem("Chat", cv.Container()))
		tabs.Append(container.NewTabItem("Contacts", cp.Content()))
		tabs.Append(container.NewTabItem("Settings", sv.Content()))
		tabs.SelectTabIndex(0)
		tabs.Show()
		contentStack.Refresh()
		wg3.Done()
	}

	loginView.usernameEntry.SetText("alice")
	loginView.passwordEntry.SetText("password123")
	loginView.handleLogin()

	done := make(chan struct{})
	go func() {
		wg3.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("login did not complete within timeout")
	}

	assert.Equal(t, 3, len(tabs.Items))
	assert.Equal(t, 0, tabs.SelectedIndex())

	for i, label := range []string{"Chat", "Contacts", "Settings"} {
		assert.Equal(t, label, tabs.Items[i].Text,
			"tab %d must be labeled %q", i, label)
	}

	tabs.SelectTabIndex(2)
	assert.Equal(t, 2, tabs.SelectedIndex())
}

func TestWindowCloseCleanup(t *testing.T) {
	a := test.NewApp()
	w := a.NewWindow("SOTE Test")

	var closed bool
	w.SetOnClosed(func() {
		closed = true
	})

	w.Close()
	assert.True(t, closed, "SetOnClosed callback must fire on window close")
	_ = a
}
