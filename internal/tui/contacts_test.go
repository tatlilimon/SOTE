package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestContactsViewRenders(t *testing.T) {
	m := NewContactsModel("alice")
	m.SetSize(80, 24)

	view := m.View().Content
	assert.Contains(t, view, "No contacts yet")
	assert.Contains(t, view, "Press 'a'")

	m.SetContacts([]contactItem{
		{username: "bob", onionAddr: "bob123.onion"},
		{username: "carol", onionAddr: "carol456.onion"},
	})
	view = m.View().Content
	assert.Contains(t, view, "bob")
	assert.Contains(t, view, "carol")
	assert.Contains(t, view, "bob123.onion")
	assert.Contains(t, view, "carol456.onion")
}

func TestAddContactForm(t *testing.T) {
	m := NewContactsModel("alice")
	m.SetSize(80, 24)

	updated, _ := m.Update(tea.KeyPressMsg{Text: "a", Code: 'a'})
	cm := updated.(ContactsModel)
	assert.Equal(t, contactStateAdding, cm.state)

	view := cm.View().Content
	assert.Contains(t, view, "Add Contact")
	assert.Contains(t, view, ".onion")
}

func TestAddContactSubmitEmpty(t *testing.T) {
	m := NewContactsModel("alice")
	m.SetSize(80, 24)

	updated, _ := m.Update(tea.KeyPressMsg{Text: "a", Code: 'a'})
	cm := updated.(ContactsModel)

	updated2, _ := cm.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	cm2 := updated2.(ContactsModel)
	assert.Contains(t, cm2.err, "cannot be empty")
}

func TestAddContactSubmitInvalid(t *testing.T) {
	m := NewContactsModel("alice")
	m.SetSize(80, 24)

	m.state = contactStateAdding
	m.input.SetValue("not-an-onion.com")

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	cm := updated.(ContactsModel)
	assert.Contains(t, cm.err, ".onion")
}

func TestAddContactSubmitValid(t *testing.T) {
	m := NewContactsModel("alice")
	m.SetSize(80, 24)

	m.state = contactStateAdding
	m.input.SetValue("xyz.onion")

	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m2 := updated.(ContactsModel)
	require.NotNil(t, cmd)

	msg := cmd()
	added, ok := msg.(ContactAddedMsg)
	require.True(t, ok)
	assert.Nil(t, added.Err)
	assert.Equal(t, "xyz.onion", added.Item.onionAddr)

	m3, _ := m2.Update(added)
	cm3 := m3.(ContactsModel)
	assert.Equal(t, contactStateViewing, cm3.state)
	assert.Len(t, cm3.contacts, 1)
	assert.Equal(t, "xyz.onion", cm3.contacts[0].onionAddr)
}

func TestEmptyState(t *testing.T) {
	m := NewContactsModel("alice")
	m.SetSize(80, 24)

	view := m.View().Content
	assert.Contains(t, view, "No contacts yet")
	assert.Contains(t, view, "add your first contact")
	assert.NotContains(t, view, "bob")
}

func TestAcceptContactRequest(t *testing.T) {
	m := NewContactsModel("alice")
	m.SetSize(80, 24)

	m.SetPendingRequests([]contactItem{
		{username: "mallory", onionAddr: "mallory.onion", pending: true},
	})

	updated, _ := m.Update(tea.KeyPressMsg{Text: "r", Code: 'r'})
	cm := updated.(ContactsModel)
	assert.Equal(t, contactStatePending, cm.state)

	view := cm.View().Content
	assert.Contains(t, view, "mallory")
	assert.Contains(t, view, "pending")

	updated2, cmd := cm.Update(tea.KeyPressMsg{Text: "a", Code: 'a'})
	_ = updated2.(ContactsModel)
	require.NotNil(t, cmd)

	msg := cmd()
	handled, ok := msg.(ContactRequestHandledMsg)
	require.True(t, ok)
	assert.True(t, handled.Accepted)
	assert.Equal(t, "mallory", handled.Username)
}

func TestDeclineContactRequest(t *testing.T) {
	m := NewContactsModel("alice")
	m.SetSize(80, 24)

	m.SetPendingRequests([]contactItem{
		{username: "mallory", onionAddr: "mallory.onion", pending: true},
	})
	m.state = contactStatePending
	m.rebuildList()

	updated, cmd := m.Update(tea.KeyPressMsg{Text: "d", Code: 'd'})
	cm := updated.(ContactsModel)
	require.NotNil(t, cmd)

	msg := cmd()
	handled, ok := msg.(ContactRequestHandledMsg)
	require.True(t, ok)
	assert.False(t, handled.Accepted)

	m2, _ := cm.Update(handled)
	cm2 := m2.(ContactsModel)
	assert.Contains(t, cm2.err, "Declined")
	assert.Len(t, cm2.pendingReqs, 0)
}

func TestContactsEscGoesBack(t *testing.T) {
	m := NewContactsModel("alice")
	m.SetSize(80, 24)

	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	require.NotNil(t, cmd)

	msg := cmd()
	_, ok := msg.(BackMsg)
	assert.True(t, ok)

	_ = updated
}

func TestContactsNavigation(t *testing.T) {
	m := NewContactsModel("alice")
	m.SetSize(80, 24)
	m.SetContacts([]contactItem{
		{username: "bob", onionAddr: "bob.onion"},
		{username: "carol", onionAddr: "carol.onion"},
	})

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	cm := updated.(ContactsModel)
	assert.Contains(t, cm.err, "bob")
}

func TestSelfAddPrevention(t *testing.T) {
	m := NewContactsModel("alice")
	m.SetSize(80, 24)

	errMsg := ContactAddedMsg{
		Err: assert.AnError,
	}
	updated, _ := m.Update(errMsg)
	cm := updated.(ContactsModel)
	assert.NotEmpty(t, cm.err)
	assert.Len(t, cm.contacts, 0)
}

func TestPendingViewEmpty(t *testing.T) {
	m := NewContactsModel("alice")
	m.SetSize(80, 24)

	updated, _ := m.Update(tea.KeyPressMsg{Text: "r", Code: 'r'})
	cm := updated.(ContactsModel)
	assert.Equal(t, contactStatePending, cm.state)

	view := cm.View().Content
	assert.Contains(t, view, "No pending")
}

func TestPendingViewEscBackToContacts(t *testing.T) {
	m := NewContactsModel("alice")
	m.SetSize(80, 24)
	m.SetContacts([]contactItem{
		{username: "bob", onionAddr: "bob.onion"},
	})

	updated, _ := m.Update(tea.KeyPressMsg{Text: "r", Code: 'r'})
	cm := updated.(ContactsModel)
	assert.Equal(t, contactStatePending, cm.state)

	updated2, _ := cm.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	cm2 := updated2.(ContactsModel)
	assert.Equal(t, contactStateViewing, cm2.state)

	view := cm2.View().Content
	assert.Contains(t, view, "bob")
}

func TestContactsLoadedMsg(t *testing.T) {
	m := NewContactsModel("alice")
	m.SetSize(80, 24)

	msg := ContactsLoadedMsg{
		Contacts: []contactItem{
			{username: "bob", onionAddr: "bob.onion"},
			{username: "carol", onionAddr: "carol.onion"},
		},
	}

	updated, _ := m.Update(msg)
	cm := updated.(ContactsModel)
	assert.Len(t, cm.contacts, 2)
	assert.Empty(t, cm.err)

	view := cm.View().Content
	assert.Contains(t, view, "bob")
	assert.Contains(t, view, "carol")
}

func TestTruncateOnion(t *testing.T) {
	short := truncateOnion("abc.onion")
	assert.Equal(t, "abc.onion", short)

	long := truncateOnion("abcdefghijklmnopqrstuvwxyz.onion")
	assert.True(t, strings.HasSuffix(long, "..."))
	assert.Len(t, long, 23)
}
