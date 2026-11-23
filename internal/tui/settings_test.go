package tui

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"sote/internal/domain"
)

type mockChanger struct {
	err             error
	changeCalled    bool
	lastUsername    string
	lastCurrentPass string
	lastNewPass     string
}

func (m *mockChanger) ChangePassword(username, current, newPass string) error {
	m.changeCalled = true
	m.lastUsername = username
	m.lastCurrentPass = current
	m.lastNewPass = newPass
	return m.err
}

func testUser(onion string) *domain.User {
	return &domain.User{
		Username:            "testuser",
		OnionAddress:        onion,
		PublicKey:           []byte("test-pub-key"),
		EncryptedPrivateKey: []byte("encrypted-key-data"),
	}
}

func TestSettingsViewRenders(t *testing.T) {
	user := testUser("abc123.onion")
	m := NewSettingsModel(user, &mockChanger{})

	view := m.View()

	assert.Contains(t, view, "testuser")
	assert.Contains(t, view, "abc123.onion")
	assert.Contains(t, view, Version)
	assert.Contains(t, view, LicenseText)
	assert.Contains(t, view, "Settings")
	assert.Contains(t, view, "User Information")
	assert.Contains(t, view, "Password")
	assert.Contains(t, view, "Connection")
	assert.Contains(t, view, "About")
}

func TestOnionAddressDisplay(t *testing.T) {
	user := testUser("xyzsecret.onion")
	m := NewSettingsModel(user, &mockChanger{})

	view := m.View()

	assert.Contains(t, view, "xyzsecret.onion")
	assert.True(t, strings.HasSuffix(user.OnionAddress, ".onion"))
}

func TestOnionNotAvailable(t *testing.T) {
	user := testUser("")
	m := NewSettingsModel(user, &mockChanger{})

	view := m.View()

	assert.Contains(t, view, "Not available")
}

func TestChangePasswordSuccess(t *testing.T) {
	changer := &mockChanger{}
	user := testUser("myaddr.onion")
	m := NewSettingsModel(user, changer)

	cmd := m.OpenPasswordForm()
	require.NotNil(t, cmd)
	msg := cmd()
	m, _ = m.Update(msg)

	m.currentInput.SetValue("oldpass")
	m.newInput.SetValue("newpass123")
	m.confirmInput.SetValue("newpass123")

	m, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	require.NotNil(t, cmd, "submit should return a command")
	msg = cmd()
	m, cmd = m.Update(msg)

	assert.True(t, changer.changeCalled)
	assert.Equal(t, "testuser", changer.lastUsername)
	assert.Equal(t, "oldpass", changer.lastCurrentPass)
	assert.Equal(t, "newpass123", changer.lastNewPass)
	assert.Empty(t, m.errorMsg)
	assert.Contains(t, m.successMsg, "successfully")
	assert.False(t, m.showPasswordForm)
}

func TestChangePasswordWrongCurrent(t *testing.T) {
	changer := &mockChanger{err: errors.New("invalid current password")}
	user := testUser("myaddr.onion")
	m := NewSettingsModel(user, changer)

	cmd := m.OpenPasswordForm()
	msg := cmd()
	m, _ = m.Update(msg)

	m.currentInput.SetValue("wrongpass")
	m.newInput.SetValue("newpass123")
	m.confirmInput.SetValue("newpass123")

	m, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	require.NotNil(t, cmd)
	msg = cmd()
	m, _ = m.Update(msg)

	assert.True(t, changer.changeCalled)
	assert.NotEmpty(t, m.errorMsg)
	assert.Contains(t, m.errorMsg, "invalid current password")
	assert.Empty(t, m.successMsg)
	assert.True(t, m.showPasswordForm)
}

func TestChangePasswordMismatch(t *testing.T) {
	changer := &mockChanger{}
	user := testUser("myaddr.onion")
	m := NewSettingsModel(user, changer)

	cmd := m.OpenPasswordForm()
	msg := cmd()
	m, _ = m.Update(msg)

	m.currentInput.SetValue("oldpass")
	m.newInput.SetValue("newpass123")
	m.confirmInput.SetValue("different")

	m, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	assert.Nil(t, cmd)
	assert.False(t, changer.changeCalled)
	assert.Contains(t, m.errorMsg, "do not match")
}

func TestChangePasswordTooShort(t *testing.T) {
	changer := &mockChanger{}
	user := testUser("myaddr.onion")
	m := NewSettingsModel(user, changer)

	cmd := m.OpenPasswordForm()
	msg := cmd()
	m, _ = m.Update(msg)

	m.currentInput.SetValue("oldpass")
	m.newInput.SetValue("short")
	m.confirmInput.SetValue("short")

	m, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	assert.Nil(t, cmd)
	assert.False(t, changer.changeCalled)
	assert.Contains(t, m.errorMsg, "at least 8")
}

func TestChangePasswordEmptyFields(t *testing.T) {
	changer := &mockChanger{}
	user := testUser("myaddr.onion")
	m := NewSettingsModel(user, changer)

	cmd := m.OpenPasswordForm()
	msg := cmd()
	m, _ = m.Update(msg)

	m, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	assert.Nil(t, cmd)
	assert.False(t, changer.changeCalled)
	assert.Contains(t, m.errorMsg, "required")
}

func TestChangePasswordSameAsCurrent(t *testing.T) {
	changer := &mockChanger{}
	user := testUser("myaddr.onion")
	m := NewSettingsModel(user, changer)

	cmd := m.OpenPasswordForm()
	msg := cmd()
	m, _ = m.Update(msg)

	m.currentInput.SetValue("samepass")
	m.newInput.SetValue("samepass")
	m.confirmInput.SetValue("samepass")

	m, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	assert.Nil(t, cmd)
	assert.False(t, changer.changeCalled)
	assert.Contains(t, m.errorMsg, "differ")
}

func TestNoPrivateKeyLeak(t *testing.T) {
	user := &domain.User{
		Username:            "alice",
		OnionAddress:        "alice123.onion",
		EncryptedPrivateKey: []byte("-----BEGIN PGP PRIVATE KEY BLOCK-----\nbase64stuff\n-----END PGP PRIVATE KEY BLOCK-----"),
		PublicKey:           []byte("pub-key-data"),
	}
	m := NewSettingsModel(user, &mockChanger{})

	view := m.View()

	assert.False(t, ExposesPrivateKey(view))
	assert.NotContains(t, view, "BEGIN PGP PRIVATE KEY")
	assert.NotContains(t, view, "PRIVATE KEY BLOCK")
}

func TestTabNavigation(t *testing.T) {
	user := testUser("addr.onion")
	m := NewSettingsModel(user, &mockChanger{})

	cmd := m.OpenPasswordForm()
	msg := cmd()
	m, _ = m.Update(msg)

	assert.Equal(t, 0, m.focusIdx)

	m, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	assert.Equal(t, 1, m.focusIdx)

	m, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	assert.Equal(t, 2, m.focusIdx)

	m, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	assert.Equal(t, 0, m.focusIdx)

	m, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	assert.Equal(t, 2, m.focusIdx)
}

func TestEscClosesPasswordForm(t *testing.T) {
	user := testUser("addr.onion")
	m := NewSettingsModel(user, &mockChanger{})

	cmd := m.OpenPasswordForm()
	msg := cmd()
	m, _ = m.Update(msg)

	assert.True(t, m.showPasswordForm)

	m, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})

	assert.False(t, m.showPasswordForm)
	assert.Empty(t, m.errorMsg)
}

func TestSetTorStatus(t *testing.T) {
	user := testUser("addr.onion")
	m := NewSettingsModel(user, &mockChanger{})

	m.SetTorStatus(true, 9060, "2h 15m")
	view := m.View()
	assert.Contains(t, view, "Connected")
	assert.Contains(t, view, "9060")
	assert.Contains(t, view, "2h 15m")

	m.SetTorStatus(false, 0, "")
	view = m.View()
	assert.Contains(t, view, "Disconnected")
	assert.Contains(t, view, "N/A")
}

func TestSettingsWindowResize(t *testing.T) {
	user := testUser("addr.onion")
	m := NewSettingsModel(user, &mockChanger{})

	m, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	assert.Equal(t, 120, m.width)
	assert.Equal(t, 40, m.height)
}
