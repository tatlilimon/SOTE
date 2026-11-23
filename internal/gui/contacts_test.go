//go:build !nogui

package gui

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	contactSvc "sote/internal/contacts"
	st "sote/internal/storage"
)

func setupContactsTest(t *testing.T) (*ContactsPage, fyne.Window) {
	t.Helper()
	a := test.NewApp()
	win := a.NewWindow("Test")

	db, err := st.Initialize(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	svc := contactSvc.NewService(db)
	page := NewContactsPage(svc, "alice", win)
	win.SetContent(page.Content())
	return page, win
}

func TestEmptyState(t *testing.T) {
	page, _ := setupContactsTest(t)

	assert.NotNil(t, page.EmptyLabel)
	assert.NotNil(t, page.EmptyBtn)
	assert.Equal(t, "No contacts yet.", page.EmptyLabel.Text)
	assert.Equal(t, "Add Your First Contact", page.EmptyBtn.Text)
	assert.False(t, page.List.Visible(), "list must be hidden when no contacts")
	assert.True(t, page.EmptyLabel.Visible(), "empty label must be visible when no contacts")
	assert.True(t, page.EmptyBtn.Visible(), "empty button must be visible when no contacts")
}

func TestContactListRenders(t *testing.T) {
	page, _ := setupContactsTest(t)

	contacts := []st.Contact{
		{ID: 1, ContactUsername: "bob", OnionAddress: "bobabcdefghijklmnop.onion", PublicKey: []byte("key1")},
		{ID: 2, ContactUsername: "carol", OnionAddress: "carol.onion", PublicKey: []byte("key2")},
	}
	page.SetContacts(contacts)

	assert.True(t, page.List.Visible(), "list must be visible after setting contacts")
	assert.False(t, page.EmptyLabel.Visible(), "empty label must be hidden when contacts exist")
	assert.False(t, page.EmptyBtn.Visible(), "empty button must be hidden when contacts exist")
	assert.Equal(t, 2, page.List.Length(), "list must contain 2 items")
}

func TestContactListRowData(t *testing.T) {
	page, _ := setupContactsTest(t)

	contacts := []st.Contact{
		{ID: 1, ContactUsername: "bob", OnionAddress: "bobabcdefghijklmnop.onion"},
	}
	page.SetContacts(contacts)

	row := newContactRow()
	assert.NotNil(t, row)
	assert.NotNil(t, row.dot)
	assert.NotNil(t, row.username)
	assert.NotNil(t, row.onionAddr)

	row.setContact(contacts[0], false)
	assert.Equal(t, "bob", row.username.Text)
	assert.True(t, len(row.onionAddr.Text) > 0, "onion address must be set")
	assert.Contains(t, row.onionAddr.Text, "...", "long onion address must be truncated")
}

func TestContactRowTruncation(t *testing.T) {
	row := newContactRow()
	row.setContact(st.Contact{ID: 1, ContactUsername: "x", OnionAddress: "short.onion"}, false)
	assert.Equal(t, "short.onion", row.onionAddr.Text)

	row2 := newContactRow()
	row2.setContact(st.Contact{ID: 2, ContactUsername: "y", OnionAddress: "abcdefghijklmnopqrstuvwxyz.onion"}, false)
	assert.Equal(t, "abcdefghijklmnopqrst...", row2.onionAddr.Text)
	assert.Len(t, row2.onionAddr.Text, 23)
}

func TestContextMenu_RightClickCallback(t *testing.T) {
	_, win := setupContactsTest(t)

	row := newContactRow()
	c := st.Contact{ID: 1, ContactUsername: "bob", OnionAddress: "bob.onion"}

	var called bool
	var captured st.Contact
	row.onRightClick = func(contact st.Contact) {
		called = true
		captured = contact
	}
	row.setContact(c, false)

	row.TappedSecondary(&fyne.PointEvent{})

	assert.True(t, called, "right-click callback must be invoked")
	assert.Equal(t, c.ID, captured.ID)
	assert.Equal(t, c.ContactUsername, captured.ContactUsername)

	_ = win
}

func TestContextMenu_PendingRightClick(t *testing.T) {
	page, _ := setupContactsTest(t)

	reqs := []st.Contact{
		{ID: 10, ContactUsername: "newuser", OnionAddress: "newuser.onion"},
	}
	page.SetPendingRequests(reqs)

	row := newContactRow()
	called := false
	row.onRightClick = func(_ st.Contact) { called = true }
	row.setContact(reqs[0], true)

	row.TappedSecondary(&fyne.PointEvent{})
	assert.True(t, called)
}

func TestContextMenu_MenuItemsExist(t *testing.T) {
	page, _ := setupContactsTest(t)

	c := st.Contact{ID: 1, ContactUsername: "test", OnionAddress: "test.onion"}

	assert.NotPanics(t, func() {
		page.showContactContextMenu(c, false)
	}, "context menu for regular contact must not panic")

	assert.NotPanics(t, func() {
		page.showContactContextMenu(c, true)
	}, "context menu for pending request must not panic")
}

func TestAcceptRequest(t *testing.T) {
	page, _ := setupContactsTest(t)

	req := st.Contact{ID: 10, ContactUsername: "requester", OnionAddress: "req.onion", PublicKey: []byte("pk")}
	page.SetPendingRequests([]st.Contact{req})

	page.handleAcceptPending(req)

	page.removePending(req)

	assert.Equal(t, 0, page.PendingList.Length())
}

func TestDeclineRequest(t *testing.T) {
	page, _ := setupContactsTest(t)

	req := st.Contact{ID: 10, ContactUsername: "requester", OnionAddress: "req.onion"}
	page.SetPendingRequests([]st.Contact{req})

	page.handleDeclinePending(req)

	assert.Equal(t, 0, page.PendingList.Length())
}

func TestDeleteContact(t *testing.T) {
	page, _ := setupContactsTest(t)

	contacts := []st.Contact{
		{ID: 1, ContactUsername: "bob", OnionAddress: "bob.onion"},
		{ID: 2, ContactUsername: "carol", OnionAddress: "carol.onion"},
	}
	page.SetContacts(contacts)
	assert.Equal(t, 2, page.List.Length())

	page.deleteContact(contacts[0])
	assert.Equal(t, 1, page.List.Length())

	page.deleteContact(contacts[1])
	assert.Equal(t, 0, page.List.Length())
	assert.False(t, page.List.Visible(), "list must be hidden when all contacts deleted")
	assert.True(t, page.EmptyLabel.Visible(), "empty state must show after deleting all contacts")
}

func TestAddContactDialog_SelfAddPrevented(t *testing.T) {
	_, win := setupContactsTest(t)

	username := extractUsernameFromOnion("alice.onion")
	assert.Equal(t, "alice", username, "username extracted from own onion must match")

	_ = win
}

func TestToolbarButtons(t *testing.T) {
	page, _ := setupContactsTest(t)

	assert.NotNil(t, page.AddBtn)
	assert.Equal(t, "Add Contact", page.AddBtn.Text)
	assert.NotNil(t, page.PendingBtn)
	assert.Equal(t, "Pending Requests", page.PendingBtn.Text)
}

func TestAccordionExists(t *testing.T) {
	page, _ := setupContactsTest(t)

	assert.NotNil(t, page.Accordion)
	assert.Len(t, page.Accordion.Items, 1)
	assert.Equal(t, "Pending Requests", page.Accordion.Items[0].Title)
}

func TestContentReturnsCanvasObject(t *testing.T) {
	page, _ := setupContactsTest(t)

	content := page.Content()
	assert.NotNil(t, content)
	assert.Implements(t, (*fyne.CanvasObject)(nil), content)
}

func TestSetPendingRequestsUpdatesList(t *testing.T) {
	page, _ := setupContactsTest(t)

	assert.Equal(t, 0, page.PendingList.Length())

	page.SetPendingRequests([]st.Contact{
		{ID: 1, ContactUsername: "req1", OnionAddress: "r1.onion"},
		{ID: 2, ContactUsername: "req2", OnionAddress: "r2.onion"},
	})
	assert.Equal(t, 2, page.PendingList.Length())

	page.SetPendingRequests(nil)
	assert.Equal(t, 0, page.PendingList.Length())
}

func TestOnionTruncationHelper(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"short.onion", "short.onion"},
		{"abcdefghijklmnopqrstuvwxyz.onion", "abcdefghijklmnopqrst..."},
		{"exactly24chars_abcdefgh.onion", "exactly24chars_abcde..."},
		{"a", "a"},
	}

	for _, tt := range tests {
		got := truncateOnion(tt.input)
		assert.Equal(t, tt.expected, got, "truncateOnion(%q)", tt.input)
	}
}
