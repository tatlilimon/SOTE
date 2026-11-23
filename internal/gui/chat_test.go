//go:build !nogui

package gui

import (
	"testing"
	"time"

	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"sote/internal/crypto"
	"sote/internal/messages"
	"sote/internal/storage"
)

func setupChatTest(t *testing.T) (*messages.Service, []storage.Contact) {
	t.Helper()

	db, err := storage.Initialize(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	cs := crypto.New()

	priv, pub, err := cs.GenerateKeyPair()
	require.NoError(t, err)

	hashed, err := cs.HashPassword("password")
	require.NoError(t, err)

	aesPriv, err := cs.EncryptAES(priv, "password")
	require.NoError(t, err)

	err = storage.SaveUser(db, "alice", hashed, aesPriv, pub, "alice.onion", "/tmp/alice.torrc")
	require.NoError(t, err)

	_, bobPub, err := cs.GenerateKeyPair()
	require.NoError(t, err)

	err = storage.SaveContact(db, "alice", "bob", "bob.onion", bobPub)
	require.NoError(t, err)

	contacts, err := storage.GetAllContacts(db, "alice")
	require.NoError(t, err)

	svc := messages.NewService(db, cs)
	svc.SetLocalOnion(func() string { return "alice.onion" })
	return svc, contacts
}

func TestChatViewRenders(t *testing.T) {
	svc, contacts := setupChatTest(t)
	cv := NewChatView(svc, "alice", "password", contacts)
	cv.waitLoads()

	require.NotNil(t, cv)
	require.NotNil(t, cv.Container())

	ctr := cv.Container()
	assert.NotNil(t, ctr)

	assert.Equal(t, "bob", cv.SelectedContact())
	assert.Empty(t, cv.Messages())
}

func TestSendMessage(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	_ = a.NewWindow("test")

	svc, contacts := setupChatTest(t)
	cv := NewChatView(svc, "alice", "password", contacts)
	cv.waitLoads()

	assert.Equal(t, "bob", cv.SelectedContact())
	assert.True(t, cv.sendBtn.Disabled())

	cv.input.SetText("Hello Bob!")

	assert.False(t, cv.sendBtn.Disabled(), "button should enable when text entered and contact selected")

	cv.sendMessage()

	assert.Empty(t, cv.input.Text)
	assert.True(t, cv.sendBtn.Disabled(), "button should disable after send clears input")

	time.Sleep(500 * time.Millisecond)

	msgs := cv.Messages()
	assert.NotEmpty(t, msgs, "messages should be loaded after send")
	if len(msgs) > 0 {
		assert.Equal(t, "Hello Bob!", msgs[len(msgs)-1].Content)
	}
}

func TestReceiveMessage(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	_ = a.NewWindow("test")

	svc, contacts := setupChatTest(t)

	require.NoError(t, svc.SendMessage(t.Context(), "alice", "bob", "password", "Hi from alice"))

	cv := NewChatView(svc, "alice", "password", contacts)
	cv.waitLoads()

	time.Sleep(500 * time.Millisecond)

	msgs := cv.Messages()
	assert.NotEmpty(t, msgs, "messages should have loaded")
	hasAlice := false
	for _, m := range msgs {
		if m.Sender == "alice" {
			hasAlice = true
			break
		}
	}
	assert.True(t, hasAlice, "should contain at least one message from alice")
}

func TestContactSwitch(t *testing.T) {
	svc, contacts := setupChatTest(t)

	cv := NewChatView(svc, "alice", "password", contacts)
	cv.waitLoads()
	assert.Equal(t, "bob", cv.SelectedContact())

	cv.SetContacts(contacts)
	assert.Equal(t, "bob", cv.SelectedContact())

	cv.SetContacts([]storage.Contact{})
	assert.Empty(t, cv.SelectedContact())
}

func TestAutoScroll(t *testing.T) {
	svc, contacts := setupChatTest(t)

	cv := NewChatView(svc, "alice", "password", contacts)
	cv.waitLoads()
	require.NotNil(t, cv.scroll)

	cv.displayMsgs = []messages.DecryptedMessage{
		{Sender: "alice", Content: "Hello", Timestamp: "2025-01-01T12:00:00Z"},
	}
	cv.refreshMessageList()

	assert.NotPanics(t, func() {
		cv.scroll.ScrollToBottom()
	})

	cv.displayMsgs = []messages.DecryptedMessage{}
	cv.refreshMessageList()

	assert.NotPanics(t, func() {
		cv.scroll.ScrollToBottom()
	})
}

func TestSendButtonDisabled_NoContact(t *testing.T) {
	_ = test.NewApp()

	svc, _ := setupChatTest(t)
	cv := NewChatView(svc, "alice", "password", nil)
	cv.waitLoads()

	assert.True(t, cv.sendBtn.Disabled())

	cv.input.SetText("Hello")
	assert.True(t, cv.sendBtn.Disabled(), "should stay disabled without contact")
}

func TestSendButtonEnabled(t *testing.T) {
	_ = test.NewApp()

	svc, contacts := setupChatTest(t)
	cv := NewChatView(svc, "alice", "password", contacts)
	cv.waitLoads()

	assert.True(t, cv.sendBtn.Disabled())

	cv.input.SetText("Hello")
	assert.False(t, cv.sendBtn.Disabled(), "should enable with contact and text")
}

func TestMessageBubbleRendering(t *testing.T) {
	svc, contacts := setupChatTest(t)

	cv := NewChatView(svc, "alice", "password", contacts)
	cv.waitLoads()

	cv.displayMsgs = []messages.DecryptedMessage{
		{Sender: "alice", Content: "Hello from me", Timestamp: "2025-01-01T12:00:00Z"},
		{Sender: "bob", Content: "Hello from bob", Timestamp: "2025-01-01T12:01:00Z"},
	}
	cv.refreshMessageList()

	objects := cv.messageBox.Objects
	assert.Len(t, objects, 2, "should have 2 message bubbles")
}

func TestEmptyStateDisplayed(t *testing.T) {
	svc, contacts := setupChatTest(t)

	cv := NewChatView(svc, "alice", "password", contacts)
	cv.waitLoads()

	cv.displayMsgs = []messages.DecryptedMessage{}
	cv.refreshMessageList()

	objects := cv.messageBox.Objects
	assert.Len(t, objects, 1)

	lbl, ok := objects[0].(*widget.Label)
	if assert.True(t, ok, "empty state should be a label") {
		assert.Contains(t, lbl.Text, "No messages yet")
	}
}

func TestFormatTimestamp(t *testing.T) {
	tests := []struct {
		name string
		ts   string
		want string
	}{
		{"RFC3339", "2025-01-15T14:30:00Z", "[14:30]"},
		{"datetime", "2025-01-15 14:30:00", "[14:30]"},
		{"RFC3339 no Z", "2025-01-15T14:30:00Z", "[14:30]"},
		{"invalid", "not-a-timestamp", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatTimestamp(tt.ts)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestOnSubmittedSendsMessage(t *testing.T) {
	_ = test.NewApp()

	svc, contacts := setupChatTest(t)
	cv := NewChatView(svc, "alice", "password", contacts)
	cv.waitLoads()

	cv.input.SetText("Enter send test")
	assert.False(t, cv.sendBtn.Disabled())

	cv.input.OnSubmitted("Enter send test")

	assert.Empty(t, cv.input.Text)
	assert.True(t, cv.sendBtn.Disabled())
}
