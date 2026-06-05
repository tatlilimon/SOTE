package tui

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sote/internal/domain"
)

func TestLoginModelRenders(t *testing.T) {
	m := NewLoginModel(nil)
	view := m.View().Content
	assert.NotEmpty(t, view)
	assert.Contains(t, view, "SOTE")
	assert.Contains(t, view, "Sign In")
}

func TestLoginModelTabNavigation(t *testing.T) {
	m := NewLoginModel(nil)
	require.True(t, m.inputs[0].Focused())
	model, _ := tea.NewProgram(m, tea.WithInput(bytes.NewBufferString("\tq")), tea.WithOutput(bytes.NewBuffer(nil))).Run()
	lm := model.(LoginModel)
	assert.False(t, lm.inputs[0].Focused())
	assert.True(t, lm.inputs[1].Focused())
}

func TestLoginModelSubmitEmpty(t *testing.T) {
	m := NewLoginModel(nil)
	model, _ := tea.NewProgram(m, tea.WithInput(bytes.NewBufferString("\rq")), tea.WithOutput(bytes.NewBuffer(nil))).Run()
	lm := model.(LoginModel)
	assert.Contains(t, lm.err, "required")
	assert.False(t, lm.done)
}

func TestLoginModelSubmitSuccess(t *testing.T) {
	mockAuth := func(username, password string) (*domain.User, error) {
		return &domain.User{Username: username, PublicKey: []byte("pk"), RawPassword: password}, nil
	}
	m := NewLoginModel(mockAuth)
	model, _ := tea.NewProgram(m, tea.WithInput(bytes.NewBufferString("alice\tsecret123\rq")), tea.WithOutput(bytes.NewBuffer(nil))).Run()
	lm := model.(LoginModel)
	assert.True(t, lm.done)
	require.NotNil(t, lm.result)
	assert.Equal(t, "alice", lm.result.Username)
}

func TestLoginModelSubmitFailure(t *testing.T) {
	mockAuth := func(username, password string) (*domain.User, error) {
		return nil, errors.New("invalid credentials")
	}
	m := NewLoginModel(mockAuth)
	model, _ := tea.NewProgram(m, tea.WithInput(bytes.NewBufferString("alice\twrong\rq")), tea.WithOutput(bytes.NewBuffer(nil))).Run()
	lm := model.(LoginModel)
	assert.Contains(t, lm.err, "invalid credentials")
	assert.False(t, lm.done)
}

func TestLoginModelEscapeQuits(t *testing.T) {
	m := NewLoginModel(nil)
	model, _ := tea.NewProgram(m, tea.WithInput(bytes.NewBufferString("\x1b")), tea.WithOutput(bytes.NewBuffer(nil))).Run()
	lm := model.(LoginModel)
	assert.True(t, lm.done)
	assert.Nil(t, lm.result)
}

func TestLoginModelResultAccessors(t *testing.T) {
	m := NewLoginModel(nil)
	assert.False(t, m.Done())
	assert.Nil(t, m.Result())
	m.done = true
	m.result = &domain.User{Username: "test"}
	assert.True(t, m.Done())
	assert.Equal(t, "test", m.Result().Username)
}

func TestRegisterModelRenders(t *testing.T) {
	m := NewRegisterModel(nil)
	view := m.View().Content
	assert.NotEmpty(t, view)
	assert.Contains(t, view, "SOTE")
	assert.Contains(t, view, "Create Account")
}

func TestRegisterModelTabNavigation(t *testing.T) {
	m := NewRegisterModel(nil)
	require.True(t, m.inputs[0].Focused())
	model, _ := tea.NewProgram(m, tea.WithInput(bytes.NewBufferString("\t\t\x03")), tea.WithOutput(bytes.NewBuffer(nil))).Run()
	rm := model.(RegisterModel)
	assert.False(t, rm.inputs[0].Focused())
	assert.True(t, rm.inputs[2].Focused())
}

func TestRegisterModelShiftTab(t *testing.T) {
	m := NewRegisterModel(nil)
	model, _ := tea.NewProgram(m, tea.WithInput(bytes.NewBufferString("\t\x1b[Z\x03")), tea.WithOutput(bytes.NewBuffer(nil))).Run()
	rm := model.(RegisterModel)
	assert.True(t, rm.inputs[0].Focused())
}

func TestRegisterModelValidation(t *testing.T) {
	tests := []struct {
		name, input, contains string
	}{
		{"empty username", "\r\x03", "Username is required"},
		{"short username", "ab\t\t\r\x03", "3-32"},
		{"weak password", "validuser\tshort\t\t\r\x03", "at least 8"},
		{"mismatch", "validuser\tpassword123\twrongpass\r\x03", "do not match"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewRegisterModel(nil)
			model, _ := tea.NewProgram(m, tea.WithInput(bytes.NewBufferString(tt.input)), tea.WithOutput(bytes.NewBuffer(nil))).Run()
			rm := model.(RegisterModel)
			assert.Contains(t, rm.err, tt.contains)
			assert.False(t, rm.done)
		})
	}
}

func TestRegisterModelSuccess(t *testing.T) {
	mockAuth := func(username, password string) (*domain.User, error) {
		return &domain.User{Username: username, PublicKey: []byte("pk"), RawPassword: password}, nil
	}
	m := NewRegisterModel(mockAuth)
	model, _ := tea.NewProgram(m, tea.WithInput(bytes.NewBufferString("alice\tsecret123\tsecret123\rq")), tea.WithOutput(bytes.NewBuffer(nil))).Run()
	rm := model.(RegisterModel)
	assert.True(t, rm.done)
	require.NotNil(t, rm.result)
	assert.Equal(t, "alice", rm.result.Username)
}

func TestRegisterModelFailure(t *testing.T) {
	mockAuth := func(username, password string) (*domain.User, error) {
		return nil, errors.New("username already exists")
	}
	m := NewRegisterModel(mockAuth)
	model, _ := tea.NewProgram(m, tea.WithInput(bytes.NewBufferString("alice\tpassword123\tpassword123\r\x03")), tea.WithOutput(bytes.NewBuffer(nil))).Run()
	rm := model.(RegisterModel)
	assert.Contains(t, rm.err, "already exists")
	assert.False(t, rm.done)
}

func TestRegisterModelEscape(t *testing.T) {
	m := NewRegisterModel(nil)
	model, _ := tea.NewProgram(m, tea.WithInput(bytes.NewBufferString("\x1b")), tea.WithOutput(bytes.NewBuffer(nil))).Run()
	rm := model.(RegisterModel)
	assert.True(t, rm.done)
	assert.Nil(t, rm.result)
}

func TestRegisterModelDoneResult(t *testing.T) {
	m := NewRegisterModel(nil)
	assert.False(t, m.Done())
	assert.Nil(t, m.Result())
	m.done = true
	m.result = &domain.User{Username: "bob"}
	assert.True(t, m.Done())
	assert.Equal(t, "bob", m.Result().Username)
}

func TestIsValidUsername(t *testing.T) {
	tests := []struct {
		username string
		valid    bool
	}{
		{"abc", true}, {"user_name", true}, {"User123", true}, {"test_user_42", true},
		{"a", false}, {"ab", false}, {"a" + strings.Repeat("x", 32), false},
		{"user-name", false}, {"user name", false}, {"user.name", false}, {"", false},
	}
	for _, tt := range tests {
		t.Run(tt.username, func(t *testing.T) {
			assert.Equal(t, tt.valid, isValidUsername(tt.username))
		})
	}
}
