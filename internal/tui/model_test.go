package tui

import (
	"context"
	"database/sql"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"sote/internal/auth"
	"sote/internal/config"
	"sote/internal/contacts"
	"sote/internal/crypto"
	"sote/internal/messages"
	"sote/internal/storage"
	"sote/internal/tor"
	"sote/internal/transport"
)

func setupTestModel(t *testing.T) (Model, *sql.DB, func()) {
	t.Helper()

	db, err := storage.Initialize(":memory:")
	require.NoError(t, err)

	cfg := config.DefaultConfig()
	cryptoSvc := crypto.New()
	authSvc := auth.New(db)
	contactSvc := contacts.NewService(db)
	msgSvc := messages.NewService(db, cryptoSvc)
	torSvc := tor.New(t.TempDir(), 0, 0)
	srv := transport.NewServer(nil)
	client := transport.NewClient()

	m := NewModel(cfg, db, authSvc, cryptoSvc, contactSvc, msgSvc, torSvc, srv, client)
	m.activeView = loginView

	cleanup := func() {
		db.Close()
	}

	return m, db, cleanup
}

func setupLoggedInModel(t *testing.T) (Model, *sql.DB, func()) {
	t.Helper()

	m, db, cleanup := setupTestModel(t)

	ctx := context.Background()
	_, err := m.authSvc.Register(ctx, "alice", "secret123")
	require.NoError(t, err)

	user, err := m.authSvc.Login(ctx, "alice", "secret123")
	require.NoError(t, err)

	m.login.done = true
	m.login.result = user
	m.checkTransitions()

	return m, db, cleanup
}

func TestFullLoginFlow(t *testing.T) {
	m, _, cleanup := setupTestModel(t)
	defer cleanup()

	ctx := context.Background()
	_, err := m.authSvc.Register(ctx, "alice", "secret123")
	require.NoError(t, err)

	v := m.View()
	assert.NotEmpty(t, v.Content)
	assert.Contains(t, v.Content, "SOTE")
	assert.Contains(t, v.Content, "Sign In")

	user, err := m.authSvc.Login(ctx, "alice", "secret123")
	require.NoError(t, err)
	require.Equal(t, "alice", user.Username)

	m.login.done = true
	m.login.result = user
	m.checkTransitions()

	assert.Equal(t, mainMenuView, m.activeView, "should transition to main menu after login")
	require.NotNil(t, m.currentUser, "currentUser should be set")
	assert.Equal(t, "alice", m.currentUser.Username)
}

func TestViewSwitchingIntegration(t *testing.T) {
	m, _, cleanup := setupLoggedInModel(t)
	defer cleanup()

	require.Equal(t, mainMenuView, m.activeView)

	views := []struct {
		key  string
		view viewState
	}{
		{"1", chatView},
		{"2", contactsView},
		{"3", settingsView},
	}
	for _, tc := range views {
		msg := tea.KeyPressMsg{Text: tc.key, Code: rune(tc.key[0])}
		md, _ := m.Update(msg)
		m = md.(Model)
		assert.Equal(t, tc.view, m.activeView)

		escMsg := tea.KeyPressMsg{Text: "esc", Code: tea.KeyEscape}
		md, _ = m.Update(escMsg)
		m = md.(Model)
		assert.Equal(t, mainMenuView, m.activeView)
	}

	qMsg := tea.KeyPressMsg{Text: "q", Code: 'q'}
	_, cmd := m.Update(qMsg)
	assert.NotNil(t, cmd, "q from main menu should return quit command")
}

func TestIncomingMessageNotification(t *testing.T) {
	m, _, cleanup := setupLoggedInModel(t)
	defer cleanup()

	m.activeView = chatView
	assert.NotNil(t, m.chat)

	initialCount := len(m.chat.Messages())

	msg := incomingMsgMsg{sender: "bob", content: []byte("encrypted blob")}
	md, _ := m.Update(msg)
	m = md.(Model)

	assert.NotNil(t, m.chat)
	finalCount := len(m.chat.Messages())
	assert.Equal(t, initialCount, finalCount)

	m.chat.contactName = "bob"
	msg2 := incomingMsgMsg{sender: "bob", content: []byte("encrypted blob")}
	_, cmd2 := m.Update(msg2)
	assert.NotNil(t, cmd2, "should return load messages command when sender matches contact")
}

func TestGracefulShutdown(t *testing.T) {
	m, _, cleanup := setupLoggedInModel(t)
	defer cleanup()

	ctrlCMsg := tea.KeyPressMsg{Text: "ctrl+c", Code: 'c', Mod: tea.ModCtrl}
	md, cmd := m.Update(ctrlCMsg)
	m = md.(Model)

	require.NotNil(t, cmd, "ctrl+c should return shutdown + quit command")
	require.True(t, m.shuttingDown, "shuttingDown should be true")
}

func TestModelWindowResize(t *testing.T) {
	m, _, cleanup := setupTestModel(t)
	defer cleanup()

	wsMsg := tea.WindowSizeMsg{Width: 120, Height: 40}
	md, _ := m.Update(wsMsg)
	m = md.(Model)

	assert.Equal(t, 120, m.width)
	assert.Equal(t, 40, m.height)
}

func TestModelViewRendersHeaderAndStatusBar(t *testing.T) {
	m, _, cleanup := setupLoggedInModel(t)
	defer cleanup()

	m.width = 80
	m.height = 24

	v := m.View()
	assert.NotEmpty(t, v.Content)
	assert.Contains(t, v.Content, "SOTE")
	assert.Contains(t, v.Content, "Main Menu")
}

func TestModelTorStatusUpdate(t *testing.T) {
	m, _, cleanup := setupTestModel(t)
	defer cleanup()

	assert.False(t, m.torOnline)

	tsm := torStartedMsg{addr: "abc123.onion"}
	md, _ := m.Update(tsm)
	m = md.(Model)

	assert.True(t, m.torOnline)
	assert.Equal(t, "abc123.onion", m.onionAddr)
}

func TestModelTorStartFailure(t *testing.T) {
	m, _, cleanup := setupTestModel(t)
	defer cleanup()

	tsm := torStartedMsg{err: assert.AnError}
	md, _ := m.Update(tsm)
	m = md.(Model)

	assert.False(t, m.torOnline)
	assert.Contains(t, m.errMsg, assert.AnError.Error())
}

func TestModelContactRequest(t *testing.T) {
	m, _, cleanup := setupLoggedInModel(t)
	defer cleanup()

	pendingBefore := len(m.contactsT.pendingReqs)

	crm := contactReqMsg{
		fromUser:      "bob",
		fromOnion:     "bob123.onion",
		fromPublicKey: []byte("bob-pubkey"),
	}
	md, _ := m.Update(crm)
	m = md.(Model)

	assert.Equal(t, pendingBefore+1, len(m.contactsT.pendingReqs))
	assert.Equal(t, "bob", m.contactsT.pendingReqs[pendingBefore].username)
	assert.True(t, m.contactsT.pendingReqs[pendingBefore].pending)
}

func TestModelOnionAddressAccessor(t *testing.T) {
	m, _, cleanup := setupTestModel(t)
	defer cleanup()

	assert.Empty(t, m.OnionAddress())

	m.onionAddr = "test123.onion"
	assert.Equal(t, "test123.onion", m.OnionAddress())
}
