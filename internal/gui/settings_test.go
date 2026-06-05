package gui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"sote/internal/domain"
)

type mockPasswordChanger struct {
	err          error
	called       bool
	lastUsername string
	lastCurrent  string
	lastNew      string
	done         chan struct{}
}

func (m *mockPasswordChanger) ChangePassword(username, current, newPass string) error {
	m.called = true
	m.lastUsername = username
	m.lastCurrent = current
	m.lastNew = newPass
	if m.done != nil {
		close(m.done)
	}
	return m.err
}

func setupTestSettings(t *testing.T, user *domain.User, changer PasswordChanger) (*Settings, fyne.Window) {
	t.Helper()
	a := test.NewApp()
	w := a.NewWindow("Test")
	st := NewSettings(w, user, changer)
	w.SetContent(st.Content())
	return st, w
}

func TestSettingsViewRenders(t *testing.T) {
	user := &domain.User{Username: "alice", OnionAddress: "abc123.onion"}
	st, _ := setupTestSettings(t, user, &mockPasswordChanger{})

	acc := st.accordion
	require.NotNil(t, acc)
	assert.Len(t, acc.Items, 4)

	titles := make([]string, len(acc.Items))
	for i, item := range acc.Items {
		titles[i] = item.Title
	}
	assert.Equal(t, "Account", titles[0])
	assert.Equal(t, "Password", titles[1])
	assert.Equal(t, "Connection", titles[2])
	assert.Equal(t, "About", titles[3])

	assert.Equal(t, "Username: alice", st.usernameLabel.Text)
	assert.Equal(t, "Onion Address: abc123.onion", st.onionLabel.Text)
	assert.NotNil(t, st.copyBtn)

	assert.NotNil(t, st.currentEntry)
	assert.True(t, st.currentEntry.Password)
	assert.NotNil(t, st.newEntry)
	assert.True(t, st.newEntry.Password)
	assert.NotNil(t, st.confirmEntry)
	assert.True(t, st.confirmEntry.Password)
	assert.NotNil(t, st.changeBtn)

	assert.Contains(t, st.versionLabel.Text, Version)
	assert.Contains(t, st.licenseLabel.Text, LicenseText)
	assert.Contains(t, st.creditLabel.Text, "Fyne")
}

func TestOnionAddressNotAvailable(t *testing.T) {
	user := &domain.User{Username: "bob"}
	st, _ := setupTestSettings(t, user, &mockPasswordChanger{})

	assert.Equal(t, "Onion Address: Not available", st.onionLabel.Text)
}

func TestCopyButtonText(t *testing.T) {
	user := &domain.User{Username: "carol", OnionAddress: "carol.onion"}
	st, _ := setupTestSettings(t, user, &mockPasswordChanger{})

	assert.Equal(t, "Copy", st.copyBtn.Text)
	assert.Equal(t, theme.ContentCopyIcon(), st.copyBtn.Icon)
}

func TestChangePasswordValidation(t *testing.T) {
	user := &domain.User{Username: "alice"}
	st, _ := setupTestSettings(t, user, &mockPasswordChanger{})

	st.currentEntry.SetText("")
	st.newEntry.SetText("")
	st.confirmEntry.SetText("")
	st.handlePasswordChange()
	assert.Contains(t, st.passStatus.Text, "required")

	st.currentEntry.SetText("old")
	st.newEntry.SetText("short")
	st.confirmEntry.SetText("short")
	st.handlePasswordChange()
	assert.Contains(t, st.passStatus.Text, "at least 8")

	st.currentEntry.SetText("old")
	st.newEntry.SetText("newpass123")
	st.confirmEntry.SetText("different")
	st.handlePasswordChange()
	assert.Contains(t, st.passStatus.Text, "do not match")

	st.currentEntry.SetText("same")
	st.newEntry.SetText("same")
	st.confirmEntry.SetText("same")
	st.handlePasswordChange()
	assert.Contains(t, st.passStatus.Text, "differ")
}

func TestChangePasswordSuccess(t *testing.T) {
	user := &domain.User{Username: "alice"}
	mock := &mockPasswordChanger{done: make(chan struct{})}
	st, _ := setupTestSettings(t, user, mock)

	st.currentEntry.SetText("oldpass")
	st.newEntry.SetText("newpass123")
	st.confirmEntry.SetText("newpass123")
	st.handlePasswordChange()

	select {
	case <-mock.done:
	case <-time.After(2 * time.Second):
		t.Fatal("password change timed out")
	}

	assert.True(t, mock.called)
	assert.Equal(t, "alice", mock.lastUsername)
	assert.Equal(t, "oldpass", mock.lastCurrent)
	assert.Equal(t, "newpass123", mock.lastNew)

	// fyne.Do runs synchronously in test driver — verify cleanup
	assert.Empty(t, st.currentEntry.Text)
	assert.Empty(t, st.newEntry.Text)
	assert.Empty(t, st.confirmEntry.Text)
	assert.Empty(t, st.passStatus.Text)
}

func TestChangePasswordWrongCurrent(t *testing.T) {
	user := &domain.User{Username: "alice"}
	mock := &mockPasswordChanger{
		err:  errors.New("current password is incorrect"),
		done: make(chan struct{}),
	}
	st, _ := setupTestSettings(t, user, mock)

	st.currentEntry.SetText("wrong")
	st.newEntry.SetText("newpass123")
	st.confirmEntry.SetText("newpass123")
	st.handlePasswordChange()

	select {
	case <-mock.done:
	case <-time.After(2 * time.Second):
		t.Fatal("password change timed out")
	}

	assert.True(t, mock.called)
	assert.Empty(t, st.currentEntry.Text)
	assert.Empty(t, st.newEntry.Text)
	assert.Empty(t, st.confirmEntry.Text)
}

func TestChangePasswordNoUser(t *testing.T) {
	mock := &mockPasswordChanger{}
	st, _ := setupTestSettings(t, nil, mock)

	st.currentEntry.SetText("old")
	st.newEntry.SetText("newpass123")
	st.confirmEntry.SetText("newpass123")
	st.handlePasswordChange()

	assert.Contains(t, st.passStatus.Text, ErrNoUserSession.Error())
	assert.False(t, mock.called)
}

func TestNoPrivateKeyLeak(t *testing.T) {
	user := &domain.User{
		Username:            "alice",
		OnionAddress:        "alice.onion",
		EncryptedPrivateKey: []byte("-----BEGIN PGP PRIVATE KEY BLOCK-----\nbase64stuff\n-----END PGP PRIVATE KEY BLOCK-----"),
		PublicKey:           []byte("pub-key-data"),
	}
	st, _ := setupTestSettings(t, user, &mockPasswordChanger{})

	// Collect all widget text visible in the accordion
	var b strings.Builder
	for _, item := range st.accordion.Items {
		extractText(item.Detail, &b)
	}
	view := b.String()

	assert.False(t, ExposesPrivateKey(view), "private key must not be exposed in settings view")
	assert.NotContains(t, view, "BEGIN PGP PRIVATE KEY")
	assert.NotContains(t, view, "PRIVATE KEY BLOCK")
	assert.NotContains(t, view, "ENCRYPTED PRIVATE KEY BLOCK")
}

func extractText(obj fyne.CanvasObject, b *strings.Builder) {
	switch o := obj.(type) {
	case *widget.Label:
		b.WriteString(o.Text)
	case *widget.Entry:
		b.WriteString(o.Text)
	case *widget.Button:
		b.WriteString(o.Text)
	case *fyne.Container:
		for _, child := range o.Objects {
			extractText(child, b)
		}
	}
}

func TestConnectionStatus(t *testing.T) {
	user := &domain.User{Username: "alice"}
	st, _ := setupTestSettings(t, user, &mockPasswordChanger{})

	st.SetTorStatus(false, 0, "")
	assert.Equal(t, "Tor Status: Disconnected", st.torStatusText.Text)
	assert.Equal(t, "SOCKS Port: N/A", st.socksLabel.Text)
	assert.Equal(t, "Uptime: N/A", st.uptimeLabel.Text)

	st.SetTorStatus(true, 9060, "2h 15m")
	assert.Equal(t, "Tor Status: Connected", st.torStatusText.Text)
	assert.Equal(t, "SOCKS Port: 9060", st.socksLabel.Text)
	assert.Equal(t, "Uptime: 2h 15m", st.uptimeLabel.Text)
}

func TestConnectionColorChange(t *testing.T) {
	user := &domain.User{Username: "alice"}
	st, _ := setupTestSettings(t, user, &mockPasswordChanger{})

	st.SetTorStatus(false, 0, "")
	disconnectedColor := st.torStatusText.Color

	st.SetTorStatus(true, 9060, "")
	connectedColor := st.torStatusText.Color

	assert.NotEqual(t, disconnectedColor, connectedColor,
		"connected and disconnected colors must differ")
}

func TestSetUser(t *testing.T) {
	user1 := &domain.User{Username: "alice", OnionAddress: "alice.onion"}
	st, _ := setupTestSettings(t, user1, &mockPasswordChanger{})

	assert.Equal(t, "Username: alice", st.usernameLabel.Text)

	st.SetUser(&domain.User{Username: "bob", OnionAddress: "bob.onion"})
	assert.Equal(t, "Username: bob", st.usernameLabel.Text)
	assert.Equal(t, "Onion Address: bob.onion", st.onionLabel.Text)
}

func TestExposesPrivateKey_Markers(t *testing.T) {
	assert.True(t, ExposesPrivateKey("-----BEGIN PGP PRIVATE KEY BLOCK-----"))
	assert.True(t, ExposesPrivateKey("some text PRIVATE KEY BLOCK here"))
	assert.False(t, ExposesPrivateKey("normal settings view"))
	assert.False(t, ExposesPrivateKey(""))
}

func TestContentReturnsAccordion(t *testing.T) {
	user := &domain.User{Username: "alice"}
	st, _ := setupTestSettings(t, user, &mockPasswordChanger{})

	assert.Same(t, st.accordion, st.Content())
}

func TestPasswordEntriesAreMasked(t *testing.T) {
	user := &domain.User{Username: "alice"}
	st, _ := setupTestSettings(t, user, &mockPasswordChanger{})

	require.NotNil(t, st.currentEntry)
	require.NotNil(t, st.newEntry)
	require.NotNil(t, st.confirmEntry)

	assert.True(t, st.currentEntry.Password, "current entry must be password-masked")
	assert.True(t, st.newEntry.Password, "new entry must be password-masked")
	assert.True(t, st.confirmEntry.Password, "confirm entry must be password-masked")
}
