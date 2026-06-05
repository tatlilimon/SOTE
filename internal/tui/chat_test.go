package tui

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"testing"

	"github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"charm.land/bubbletea/v2"

	"sote/internal/messages"
	"sote/internal/storage"
)

type mockCrypto struct {
	keyPairs map[string]struct{ priv, pub []byte }
}

func newMockCrypto() *mockCrypto {
	return &mockCrypto{keyPairs: make(map[string]struct{ priv, pub []byte })}
}

func (m *mockCrypto) generateAndStore(username string) ([]byte, []byte, error) {
	keyRing, err := crypto.GenerateKey(username, "", "rsa", 2048)
	if err != nil {
		return nil, nil, err
	}
	priv, err := keyRing.Armor()
	if err != nil {
		return nil, nil, err
	}
	pub, err := keyRing.GetArmoredPublicKey()
	if err != nil {
		return nil, nil, err
	}
	m.keyPairs[username] = struct{ priv, pub []byte }{[]byte(priv), []byte(pub)}
	return []byte(priv), []byte(pub), nil
}

func (m *mockCrypto) HashPassword(password string) (string, error) {
	data := []byte(password)
	for i := 0; i < 21; i++ {
		h := sha256.Sum256(data)
		data = h[:]
	}
	return fmt.Sprintf("%x", data), nil
}

func (m *mockCrypto) VerifyPassword(hash, password string) error {
	computed, err := m.HashPassword(password)
	if err != nil {
		return err
	}
	if hash != computed {
		return errors.New("password mismatch")
	}
	return nil
}

func (m *mockCrypto) GenerateKeyPair() ([]byte, []byte, error) {
	keyRing, err := crypto.GenerateKey("test", "", "rsa", 2048)
	if err != nil {
		return nil, nil, err
	}
	priv, err := keyRing.Armor()
	if err != nil {
		return nil, nil, err
	}
	pub, err := keyRing.GetArmoredPublicKey()
	if err != nil {
		return nil, nil, err
	}
	return []byte(priv), []byte(pub), nil
}

func (m *mockCrypto) EncryptAES(data []byte, passphrase string) ([]byte, error) {
	key := sha256.Sum256([]byte(passphrase))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, data, nil), nil
}

func (m *mockCrypto) DecryptAES(data []byte, passphrase string) ([]byte, error) {
	key := sha256.Sum256([]byte(passphrase))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}
	return gcm.Open(nil, data[:nonceSize], data[nonceSize:], nil)
}

func (m *mockCrypto) EncryptMessage(plaintext, publicKey []byte) ([]byte, error) {
	key, err := crypto.NewKeyFromArmored(string(publicKey))
	if err != nil {
		return nil, err
	}
	keyRing, err := crypto.NewKeyRing(key)
	if err != nil {
		return nil, err
	}
	enc, err := keyRing.Encrypt(crypto.NewPlainMessage(plaintext), nil)
	if err != nil {
		return nil, err
	}
	armored, _ := enc.GetArmored()
	return []byte(armored), nil
}

func (m *mockCrypto) DecryptMessage(ciphertext, privateKey []byte, passphrase string) (string, error) {
	decryptedKey, err := m.DecryptAES(privateKey, passphrase)
	if err != nil {
		return "", fmt.Errorf("decrypt private key: %w", err)
	}
	key, err := crypto.NewKeyFromArmored(string(decryptedKey))
	if err != nil {
		return "", err
	}
	keyRing, err := crypto.NewKeyRing(key)
	if err != nil {
		return "", err
	}
	msg, err := crypto.NewPGPMessageFromArmored(string(ciphertext))
	if err != nil {
		return "", err
	}
	dec, err := keyRing.Decrypt(msg, nil, 0)
	if err != nil {
		return "", err
	}
	return string(dec.GetBinary()), nil
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

	svc := messages.NewService(db, mc)
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

	svc := messages.NewService(db, mc)
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

	svc := messages.NewService(db, mc)
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

	svc := messages.NewService(db, mc)
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

	svc := messages.NewService(db, mc)
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

	svc := messages.NewService(db, mc)
	err := svc.SendMessage(context.Background(), "alice", "bob", "alicepass", "GPG secret")
	require.NoError(t, err)

	rows, err := storage.GetMessages(db, "alice", "bob")
	require.NoError(t, err)
	var gpgPayload []byte
	for _, row := range rows {
		if len(row.Message) > 1 && row.Message[0] == 0x01 {
			gpgPayload = row.Message[1:]
			break
		}
	}
	require.NotNil(t, gpgPayload)

	err = svc.ReceiveMessage(context.Background(), "alice", "bob", gpgPayload)
	require.NoError(t, err)

	msgs, err := svc.FetchMessages(context.Background(), "alice", "bob", "alicepass")
	require.NoError(t, err)
	m.Update(msgsLoadedMsg{messages: msgs})

	found := false
	for _, dm := range m.Messages() {
		if dm.Content == "GPG secret" {
			found = true
			break
		}
	}
	assert.True(t, found, "GPG-encrypted received message should be decrypted")
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
