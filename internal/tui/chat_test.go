package tui

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"

	"charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"sote/internal/crypto"
	"sote/internal/messages"
	"sote/internal/storage"
)

// mockCrypto is the real crypto service with two shortcuts for speed: key
// pairs are generated once per username for the whole package, and password
// hashing uses bcrypt's minimum cost. Everything else (AES layout, unlocking,
// signatures) is the production code, which message sending now needs.
type mockCrypto struct {
	*crypto.Service
}

var keyCache sync.Map // username -> [2][]byte{priv, pub}

func newMockCrypto() *mockCrypto { return &mockCrypto{Service: crypto.New()} }

func (m *mockCrypto) generateAndStore(username string) ([]byte, []byte, error) {
	if v, ok := keyCache.Load(username); ok {
		kp := v.([2][]byte)
		return kp[0], kp[1], nil
	}
	priv, pub, err := crypto.GenerateKeyPairFor("sote", username+"@sote.local")
	if err != nil {
		return nil, nil, err
	}
	keyCache.Store(username, [2][]byte{priv, pub})
	return priv, pub, nil
}

func (m *mockCrypto) HashPassword(password string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	return string(h), err
}

// testOnion is the local onion address used by message services in tests.
const testOnion = "aliceaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.onion"

// newSvc returns a message service that can send (it knows a local onion).
func newSvc(db *sql.DB, mc *mockCrypto) *messages.Service {
	svc := messages.NewService(db, mc)
	svc.SetLocalOnion(func() string { return testOnion })
	return svc
}

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := storage.Initialize(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return db
}

func seedUser(t *testing.T, db *sql.DB, mc *mockCrypto, username, password string) ([]byte, []byte) {
	t.Helper()
	priv, pub, err := mc.generateAndStore(username)
	require.NoError(t, err)
	hashed, err := mc.HashPassword(password)
	require.NoError(t, err)
	aesPriv, err := mc.EncryptAES(priv, password)
	require.NoError(t, err)
	err = storage.SaveUser(db, username, hashed, aesPriv, pub, "onion-"+username+".onion", "/tmp/"+username+".torrc")
	require.NoError(t, err)
	return priv, pub
}

func seedContact(t *testing.T, db *sql.DB, owner, contactUsername, onion string, publicKey []byte) {
	t.Helper()
	err := storage.SaveContact(db, owner, contactUsername, onion, publicKey)
	require.NoError(t, err)
}

func setupChatModel(t *testing.T) (*ChatModel, *sql.DB, *mockCrypto) {
	t.Helper()
	db := setupTestDB(t)
	mc := newMockCrypto()

	seedUser(t, db, mc, "alice", "alicepass")
	_, bobPub := seedUser(t, db, mc, "bob", "bobpass")
	seedContact(t, db, "alice", "bob", "bob.onion", bobPub)
	_, carolPub := seedUser(t, db, mc, "carol", "carolpass")
	seedContact(t, db, "alice", "carol", "carol.onion", carolPub)

	m := NewChatModel(db, mc, "alice", "alicepass")
	m.SetService(newSvc(db, mc))
	m.SetSize(100, 30)
	_ = m.Focus()
	return m, db, mc
}

func keyMsg(code rune, text string) tea.KeyMsg {
	return tea.KeyPressMsg(tea.Key{Code: code, Text: text})
}

func TestNewChatModel(t *testing.T) {
	m, _, _ := setupChatModel(t)
	assert.True(t, m.ready)
	assert.Equal(t, "alice", m.username)
	assert.Len(t, m.contacts, 2)
	assert.Equal(t, "bob", m.contactName)
}

func TestSendMessage(t *testing.T) {
	m, db, mc := setupChatModel(t)

	m.textarea.SetValue("Hello Bob!")
	_, cmd := m.Update(keyMsg(tea.KeyEnter, "enter"))
	require.NotNil(t, cmd)

	result := cmd()
	msg, ok := result.(msgSentMsg)
	require.True(t, ok)
	assert.NoError(t, msg.err)

	assert.Empty(t, m.TextareaValue())

	svc := newSvc(db, mc)
	msgs, err := svc.FetchMessages(context.Background(), "alice", "bob", "alicepass")
	require.NoError(t, err)
	found := false
	for _, dm := range msgs {
		if dm.Content == "Hello Bob!" {
			found = true
			assert.Equal(t, "alice", dm.Sender)
			break
		}
	}
	assert.True(t, found, "sent message should be in DB")
}

func TestReceiveMessage(t *testing.T) {
	m, db, mc := setupChatModel(t)

	svc := newSvc(db, mc)
	sendAndDeliver(t, svc, "alice", "bob", "alicepass", "Hey from Bob")
	sendAndDeliver(t, svc, "alice", "bob", "alicepass", "How are you?")

	msgs, err := svc.FetchMessages(context.Background(), "alice", "bob", "alicepass")
	require.NoError(t, err)
	_, cmd := m.Update(msgsLoadedMsg{messages: msgs})
	assert.Nil(t, cmd)

	displayed := m.Messages()
	assert.GreaterOrEqual(t, len(displayed), 1)

	hasContent := false
	for _, dm := range displayed {
		if dm.Content == "Hey from Bob" {
			hasContent = true
			assert.Equal(t, "alice", dm.Sender)
		}
	}
	assert.True(t, hasContent, "decrypted message content should be present")
}

func TestSwitchContact(t *testing.T) {
	m, _, _ := setupChatModel(t)

	assert.Equal(t, "bob", m.contactName)

	_, cmd := m.Update(keyMsg(tea.KeyRight, "right"))
	assert.Equal(t, "carol", m.contactName)
	assert.True(t, m.Loading())
	assert.NotNil(t, cmd)

	_, cmd = m.Update(keyMsg(tea.KeyRight, "right"))
	assert.Equal(t, "bob", m.contactName)

	_, cmd = m.Update(keyMsg(tea.KeyLeft, "left"))
	assert.Equal(t, "carol", m.contactName)

	_, cmd = m.Update(keyMsg(tea.KeyLeft, "left"))
	assert.Equal(t, "bob", m.contactName)
	_ = cmd
}

func TestSwitchContactClearsMessages(t *testing.T) {
	m, db, mc := setupChatModel(t)

	svc := newSvc(db, mc)
	sendAndDeliver(t, svc, "alice", "bob", "alicepass", "msg with bob")
	msgs, _ := svc.FetchMessages(context.Background(), "alice", "bob", "alicepass")
	m.Update(msgsLoadedMsg{messages: msgs})
	assert.NotEmpty(t, m.Messages())

	_, cmd := m.Update(keyMsg(tea.KeyRight, "right"))
	assert.Empty(t, m.Messages())
	assert.True(t, m.Loading())
	_ = cmd
}

func TestAutoScroll(t *testing.T) {
	m, db, mc := setupChatModel(t)

	svc := newSvc(db, mc)
	sendAndDeliver(t, svc, "alice", "bob", "alicepass", "msg 1")
	sendAndDeliver(t, svc, "alice", "bob", "alicepass", "msg 2")
	sendAndDeliver(t, svc, "alice", "bob", "alicepass", "msg 3")

	msgs, err := svc.FetchMessages(context.Background(), "alice", "bob", "alicepass")
	require.NoError(t, err)

	m.Update(msgsLoadedMsg{messages: msgs})
	assert.True(t, m.viewport.AtBottom(), "viewport should be at bottom after loading messages")
}

func TestOwnMessageDecryption(t *testing.T) {
	m, db, mc := setupChatModel(t)

	svc := newSvc(db, mc)
	sendAndDeliver(t, svc, "alice", "bob", "alicepass", "secret from me")

	msgs, err := svc.FetchMessages(context.Background(), "alice", "bob", "alicepass")
	require.NoError(t, err)
	m.Update(msgsLoadedMsg{messages: msgs})

	foundOwn := false
	for _, dm := range m.Messages() {
		if dm.Content == "secret from me" && dm.Sender == "alice" {
			foundOwn = true
			break
		}
	}
	assert.True(t, foundOwn, "own AES-encrypted message should be displayed decrypted")
}

func TestReceivedMessageDecryption(t *testing.T) {
	m, db, mc := setupChatModel(t)

	// What the node stores for a verified envelope from bob: the PGP message
	// encrypted to alice and signed by bob.
	bobPriv, _, err := mc.generateAndStore("bob")
	require.NoError(t, err)
	bob, err := crypto.IdentityFromArmoredPrivateKey(bobPriv)
	require.NoError(t, err)
	defer bob.Close()
	_, alicePub, err := mc.generateAndStore("alice")
	require.NoError(t, err)
	payload, err := bob.EncryptFor(alicePub, []byte("GPG secret"))
	require.NoError(t, err)
	_, err = storage.SaveMessageMeta(context.Background(), db, "bob", "alice",
		append([]byte{storage.BlobReceived}, payload...), "", "")
	require.NoError(t, err)

	svc := newSvc(db, mc)
	msgs, err := svc.FetchMessages(context.Background(), "alice", "bob", "alicepass")
	require.NoError(t, err)
	m.Update(msgsLoadedMsg{messages: msgs})

	found := false
	for _, dm := range m.Messages() {
		if dm.Content == "GPG secret" {
			found = true
			assert.True(t, dm.Verified, "signed by the stored contact key")
			break
		}
	}
	assert.True(t, found, "GPG-encrypted received message should be decrypted")
}

func TestFormatMessageShowsDeliveryAndUnverified(t *testing.T) {
	m, _, _ := setupChatModel(t)
	assert.Contains(t, m.formatMessage(messages.DecryptedMessage{Sender: "alice", Content: "x", Delivery: storage.DeliveryPending}), "…")
	assert.Contains(t, m.formatMessage(messages.DecryptedMessage{Sender: "alice", Content: "x", Delivery: storage.DeliveryDelivered}), "✓")
	assert.Contains(t, m.formatMessage(messages.DecryptedMessage{Sender: "alice", Content: "x", Delivery: storage.DeliveryRejected}), "not accepted")
	assert.Contains(t, m.formatMessage(messages.DecryptedMessage{Sender: "bob", Content: "x", Verified: false}), "⚠")
	assert.NotContains(t, m.formatMessage(messages.DecryptedMessage{Sender: "bob", Content: "x", Verified: true}), "⚠")
}

func TestFocusClearsPreviousEscape(t *testing.T) {
	m, _, _ := setupChatModel(t)
	m.Update(keyMsg(tea.KeyEsc, "esc"))
	require.True(t, m.Quitting())
	_ = m.Focus()
	assert.False(t, m.Quitting(), "re-entering the chat must not bounce straight back to the menu")
}

func TestQuitOnEscape(t *testing.T) {
	m, _, _ := setupChatModel(t)

	_, cmd := m.Update(keyMsg(tea.KeyEsc, "esc"))
	assert.True(t, m.Quitting())
	assert.Nil(t, cmd)
}

func TestNoSendWhenNoContact(t *testing.T) {
	db := setupTestDB(t)
	mc := newMockCrypto()
	seedUser(t, db, mc, "alice", "alicepass")

	m := NewChatModel(db, mc, "alice", "alicepass")
	m.SetSize(100, 30)
	_ = m.Focus()

	m.textarea.SetValue("hello nobody")
	_, cmd := m.Update(keyMsg(tea.KeyEnter, "enter"))
	assert.Nil(t, cmd, "no send command when no contact selected")
}

func TestNoSendEmptyMessage(t *testing.T) {
	m, _, _ := setupChatModel(t)

	m.textarea.SetValue("")
	_, cmd := m.Update(keyMsg(tea.KeyEnter, "enter"))
	assert.Nil(t, cmd, "empty text should not send")
}

func TestFormatMessageOwnVsReceived(t *testing.T) {
	m, _, _ := setupChatModel(t)

	own := m.formatMessage(messages.DecryptedMessage{
		Sender: "alice", Content: "my message", Timestamp: "2024-01-01T12:00:00Z",
	})
	assert.Contains(t, own, "my message")
	assert.NotContains(t, own, "alice:")

	received := m.formatMessage(messages.DecryptedMessage{
		Sender: "bob", Content: "bob's message", Timestamp: "2024-01-01T12:00:00Z",
	})
	assert.Contains(t, received, "bob's message")
	assert.Contains(t, received, "bob:")
}

func TestFormatTimestamp(t *testing.T) {
	ts := formatTimestamp("2024-06-15T14:30:00Z")
	assert.Contains(t, ts, "14:30")

	ts2 := formatTimestamp("2024-01-01 09:05:00")
	assert.Contains(t, ts2, "09:05")

	ts3 := formatTimestamp("")
	assert.Empty(t, ts3)
}

func TestEmptyContactState(t *testing.T) {
	db := setupTestDB(t)
	mc := newMockCrypto()
	seedUser(t, db, mc, "alice", "alicepass")

	m := NewChatModel(db, mc, "alice", "alicepass")
	m.SetSize(100, 30)

	view := m.View()
	assert.Contains(t, view.Content, "No contacts")
}

func TestViewRenders(t *testing.T) {
	m, _, _ := setupChatModel(t)

	view := m.View()
	assert.NotEmpty(t, view.Content)
	assert.Contains(t, view.Content, "bob")
	assert.Contains(t, view.Content, "Enter: send")
}

func TestBlurAndFocus(t *testing.T) {
	m, _, _ := setupChatModel(t)
	assert.True(t, m.focused)

	m.Blur()
	assert.False(t, m.focused)

	m.textarea.SetValue("ignored")
	_, cmd := m.Update(keyMsg(tea.KeyEnter, "enter"))
	assert.Nil(t, cmd)
	assert.NotEmpty(t, m.TextareaValue())

	_ = m.Focus()
	assert.True(t, m.focused)

	m.textarea.SetValue("hello")
	_, cmd = m.Update(keyMsg(tea.KeyEnter, "enter"))
	assert.NotNil(t, cmd)
}

func TestRenderMessagesEmpty(t *testing.T) {
	m, _, _ := setupChatModel(t)

	m.renderMessages()
	content := m.viewport.View()
	assert.Contains(t, content, "No messages yet")
}

func TestRenderMessagesWithContent(t *testing.T) {
	m, _, _ := setupChatModel(t)

	m.displayMsgs = []messages.DecryptedMessage{
		{Sender: "alice", Content: "Hello", Timestamp: "2024-01-01T12:00:00Z"},
		{Sender: "bob", Content: "Hi", Timestamp: "2024-01-01T12:01:00Z"},
	}
	m.renderMessages()

	content := m.viewport.View()
	assert.Contains(t, content, "Hello")
	assert.NotContains(t, content, "No messages yet")
}

func TestErrorOnMessageLoad(t *testing.T) {
	m, _, _ := setupChatModel(t)

	m.Update(msgsLoadedMsg{err: errors.New("db failure")})
	assert.Contains(t, m.Error(), "db failure")
}

func TestErrorOnMessageSend(t *testing.T) {
	m, _, _ := setupChatModel(t)

	m.textarea.SetValue("test")
	m.contactName = "bob"

	m.Update(msgSentMsg{err: errors.New("send failure")})
	assert.Contains(t, m.Error(), "send failure")
}

func TestSetContactsSelectsFirst(t *testing.T) {
	db := setupTestDB(t)
	mc := newMockCrypto()
	seedUser(t, db, mc, "alice", "alicepass")

	m := NewChatModel(db, mc, "alice", "alicepass")
	assert.Empty(t, m.contactName)

	_, bobPub := seedUser(t, db, mc, "bob", "bobpass")
	seedContact(t, db, "alice", "bob", "bob.onion", bobPub)

	contacts, err := storage.GetAllContacts(db, "alice")
	require.NoError(t, err)

	m.SetContacts(contacts)
	assert.Equal(t, "bob", m.contactName)
}

func TestLoadMessagesCmdWithNoContact(t *testing.T) {
	db := setupTestDB(t)
	mc := newMockCrypto()
	seedUser(t, db, mc, "alice", "alicepass")

	m := NewChatModel(db, mc, "alice", "alicepass")
	cmd := m.loadMessagesCmd()
	assert.Nil(t, cmd)
}

func TestWindowResize(t *testing.T) {
	m, _, _ := setupChatModel(t)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	assert.Equal(t, 120, m.width)
	assert.Equal(t, 40, m.height)
	assert.Equal(t, 120, m.viewport.Width())
	assert.Greater(t, m.viewport.Height(), 0)
}

func TestSendOnEnterWithContent(t *testing.T) {
	m, _, _ := setupChatModel(t)

	m.textarea.SetValue("hello world")
	_, cmd := m.Update(keyMsg(tea.KeyEnter, "enter"))

	assert.NotNil(t, cmd)
	assert.Empty(t, m.TextareaValue())
}

func TestLoadingFlagAfterMessageSent(t *testing.T) {
	m, _, _ := setupChatModel(t)

	m.textarea.SetValue("test")
	_, cmd := m.Update(keyMsg(tea.KeyEnter, "enter"))
	assert.NotNil(t, cmd)

	result := cmd()
	sendMsg := result.(msgSentMsg)
	assert.NoError(t, sendMsg.err)

	_, reloadCmd := m.Update(msgSentMsg{err: nil})
	assert.NotNil(t, reloadCmd)
	assert.True(t, m.Loading())
}

func sendAndDeliver(t *testing.T, svc *messages.Service, sender, receiver, password, plaintext string) {
	t.Helper()
	err := svc.SendMessage(context.Background(), sender, receiver, password, plaintext)
	require.NoError(t, err)
}
