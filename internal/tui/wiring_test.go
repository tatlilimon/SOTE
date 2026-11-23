package tui

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"sote/internal/integration"
	"sote/internal/tor"
)

func validOnion(t *testing.T) string {
	t.Helper()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	return tor.OnionAddressForKey(key)
}

func press(m Model, keys ...tea.KeyMsg) Model {
	for _, k := range keys {
		next, cmd := m.Update(k)
		m = next.(Model)
		// Run synchronous commands that only return messages for the model.
		if cmd != nil {
			if msg := cmd(); msg != nil {
				if _, isBatch := msg.(tea.BatchMsg); !isBatch {
					next, _ = m.Update(msg)
					m = next.(Model)
				}
			}
		}
	}
	return m
}

func runeKey(r rune) tea.KeyMsg { return keyMsg(r, string(r)) }

func TestTypingQInChatStaysInChat(t *testing.T) {
	m, _, cleanup := setupLoggedInModel(t)
	defer cleanup()

	m = press(m, runeKey('1'))
	require.Equal(t, chatView, m.activeView)

	// q used to send the user back to the menu, and the chat box never had
	// focus, so nothing could be typed.
	m = press(m, runeKey('q'), runeKey('u'), runeKey('i'))
	assert.Equal(t, chatView, m.activeView)
	assert.Equal(t, "qui", m.chat.TextareaValue())

	m = press(m, keyMsg(tea.KeyEsc, "esc"))
	assert.Equal(t, mainMenuView, m.activeView)

	// Coming back works (Esc used to leave the chat stuck in "quitting").
	m = press(m, runeKey('1'))
	assert.Equal(t, chatView, m.activeView)
}

func TestAddContactFieldReceivesTyping(t *testing.T) {
	cm := NewContactsModel("alice")
	cm.SetSize(80, 24)
	m := cm
	next, _ := m.Update(runeKey('a'))
	m = next.(ContactsModel)
	require.Equal(t, contactStateAdding, m.state)

	for _, r := range "abc.onion" {
		next, _ = m.Update(runeKey(r))
		m = next.(ContactsModel)
	}
	assert.Equal(t, "abc.onion", m.input.Value(), "typed characters reach the address field")
}

func TestAddContactUsesActionAndShowsProgress(t *testing.T) {
	cm := NewContactsModel("alice")
	var got string
	cm.SetActions(ContactActions{Add: func(onion string) (contactItem, error) {
		got = onion
		return contactItem{username: "bob", onionAddr: onion}, nil
	}})
	onion := validOnion(t)
	m := cm
	next, _ := m.Update(runeKey('a'))
	m = next.(ContactsModel)
	m.input.SetValue("  https://" + onion + "/ ")
	next, cmd := m.Update(keyMsg(tea.KeyEnter, "enter"))
	m = next.(ContactsModel)
	require.NotNil(t, cmd)
	assert.NotEmpty(t, m.Busy())

	next, _ = m.Update(cmd())
	m = next.(ContactsModel)
	assert.Equal(t, onion, got, "the pasted address is normalized")
	assert.Empty(t, m.Busy())
	require.Len(t, m.contacts, 1)
	assert.Equal(t, "bob", m.contacts[0].username)
}

func TestAddContactRejectsInvalidOnion(t *testing.T) {
	cm := NewContactsModel("alice")
	cm.SetActions(ContactActions{Add: func(string) (contactItem, error) {
		t.Fatal("must not contact an invalid address")
		return contactItem{}, nil
	}})
	m := cm
	next, _ := m.Update(runeKey('a'))
	m = next.(ContactsModel)
	m.input.SetValue("short.onion")
	next, cmd := m.Update(keyMsg(tea.KeyEnter, "enter"))
	m = next.(ContactsModel)
	assert.Nil(t, cmd)
	assert.Contains(t, m.err, "v3 onion")
}

func TestAcceptFailureIsShown(t *testing.T) {
	cm := NewContactsModel("alice")
	cm.SetActions(ContactActions{Accept: func(int64) (contactItem, error) {
		return contactItem{}, errors.New("the requester's onion address serves a different key")
	}})
	cm.SetPendingRequests([]contactItem{{username: "mallory", pending: true, requestID: 7}})
	m := cm
	next, _ := m.Update(runeKey('r'))
	m = next.(ContactsModel)
	next, cmd := m.Update(runeKey('a'))
	m = next.(ContactsModel)
	require.NotNil(t, cmd)
	next, _ = m.Update(cmd())
	m = next.(ContactsModel)
	assert.Contains(t, m.err, "different key")
	assert.Len(t, m.pendingReqs, 1, "a failed accept keeps the request")
}

func TestSettingsEnterOpensPasswordFormAndSessionGetsNewPassword(t *testing.T) {
	user := testUser("x.onion")
	user.RawPassword = "old-password"
	sm := NewSettingsModel(user, &mockChanger{})
	sm.SetSize(80, 40)

	sm, _ = sm.Update(keyMsg(tea.KeyEnter, "enter"))
	require.True(t, sm.showPasswordForm, "Enter opens the form (it could not be opened before)")

	sm.currentInput.SetValue("old-password")
	sm.newInput.SetValue("new-password-1")
	sm.confirmInput.SetValue("new-password-1")
	sm, cmd := sm.Update(keyMsg(tea.KeyEnter, "enter"))
	require.NotNil(t, cmd)
	sm, _ = sm.Update(cmd())

	assert.Equal(t, "new-password-1", user.RawPassword, "the session used to be left with an empty password")
	assert.False(t, sm.showPasswordForm)
}

func TestBridgesEditorSavesThroughSaver(t *testing.T) {
	sm := NewSettingsModel(testUser(""), &mockChanger{})
	var savedUse bool
	var savedText string
	sm.SetBridges(false, "", 0, func(use bool, text string) error {
		savedUse, savedText = use, text
		return nil
	})
	sm, _ = sm.Update(runeKey('b'))
	require.True(t, sm.showBridges)
	sm.bridgesArea.SetValue("192.0.2.1:9001")
	sm, _ = sm.Update(keyMsg('t', "ctrl+t"))
	sm, cmd := sm.Update(keyMsg('s', "ctrl+s"))
	require.NotNil(t, cmd)
	sm, _ = sm.Update(cmd())
	assert.True(t, savedUse)
	assert.Equal(t, "192.0.2.1:9001", savedText)
	assert.False(t, sm.showBridges)
	assert.Contains(t, sm.bridgesNotice, "Restart")
}

func TestNodeEventsReachTheModel(t *testing.T) {
	m, _, cleanup := setupLoggedInModel(t)
	defer cleanup()

	next, _ := m.Update(NodeEventMsg{Event: integration.Event{Kind: integration.EventTorFailed, Detail: "no network"}})
	m = next.(Model)
	assert.Contains(t, m.errMsg, "no network")
	assert.False(t, m.torOnline)

	next, _ = m.Update(NodeEventMsg{Event: integration.Event{Kind: integration.EventMessageReceived, Peer: "carol"}})
	m = next.(Model)
	assert.Contains(t, m.notice, "carol")
}
