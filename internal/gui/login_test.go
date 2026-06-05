package gui

import (
	"context"
	"sync"
	"testing"
	"time"

	"fyne.io/fyne/v2/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"sote/internal/auth"
	"sote/internal/domain"
	"sote/internal/storage"
)

func setupLoginView(t *testing.T) (*LoginView, *auth.AuthService) {
	t.Helper()
	app := test.NewApp()
	win := app.NewWindow("Test")

	db, err := storage.Initialize(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	authSvc := auth.New(db)
	lv := NewLoginView(win, authSvc)
	return lv, authSvc
}

func TestLoginFormRenders(t *testing.T) {
	lv, _ := setupLoginView(t)

	assert.NotNil(t, lv.usernameEntry, "username entry must exist")
	assert.NotNil(t, lv.passwordEntry, "password entry must exist")
	assert.NotNil(t, lv.loginBtn, "login button must exist")
	assert.NotNil(t, lv.registerBtn, "register button must exist")
	assert.NotNil(t, lv.switchLink, "switch link must exist")
	assert.NotNil(t, lv.errorLabel, "error label must exist")
	assert.NotNil(t, lv.progress, "progress bar must exist")

	assert.False(t, lv.IsRegister(), "initial mode must be login")
	assert.Equal(t, "Login", lv.loginBtn.Text, "login button text must be Login")
	assert.Equal(t, "Create Account", lv.switchLink.Text, "switch link text must be Create Account")
	assert.False(t, lv.loginBtn.Disabled(), "login button must be enabled initially")

	content := lv.Content()
	require.NotNil(t, content, "content must not be nil")
}

func TestLoginSuccess(t *testing.T) {
	lv, authSvc := setupLoginView(t)

	_, err := authSvc.Register(context.Background(), "bob", "mypassword123")
	require.NoError(t, err)

	lv.usernameEntry.SetText("bob")
	lv.passwordEntry.SetText("mypassword123")

	var capturedUser *domain.User
	var wg sync.WaitGroup
	wg.Add(1)
	lv.OnLoginSuccess = func(user *domain.User) {
		capturedUser = user
		wg.Done()
	}

	lv.handleLogin()

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		assert.NotNil(t, capturedUser, "OnLoginSuccess must be called with a user")
		assert.Equal(t, "bob", capturedUser.Username)
	case <-time.After(5 * time.Second):
		t.Fatal("login did not complete within timeout")
	}
}

func TestLoginEmptyFields(t *testing.T) {
	lv, _ := setupLoginView(t)

	lv.usernameEntry.SetText("")
	lv.passwordEntry.SetText("")
	lv.handleLogin()

	assert.Contains(t, lv.Error(), "Username is required")

	lv.usernameEntry.SetText("user")
	lv.passwordEntry.SetText("")
	lv.handleLogin()

	assert.Contains(t, lv.Error(), "Password is required")
}

func TestLoginWrongPassword(t *testing.T) {
	lv, authSvc := setupLoginView(t)

	_, err := authSvc.Register(context.Background(), "eve", "correctpass123")
	require.NoError(t, err)

	lv.usernameEntry.SetText("eve")
	lv.passwordEntry.SetText("wrongpassword")

	lv.OnLoginSuccess = func(_ *domain.User) {
		t.Error("OnLoginSuccess must not be called for wrong password")
	}

	lv.handleLogin()

	time.Sleep(200 * time.Millisecond)

	assert.Contains(t, lv.Error(), "invalid username or password")
	assert.False(t, lv.Loading(), "loading must be false after failed auth")
}

func TestRegisterPasswordMismatch(t *testing.T) {
	lv, _ := setupLoginView(t)

	lv.toggleMode()
	assert.True(t, lv.IsRegister(), "must be in register mode after toggle")

	lv.usernameEntry.SetText("newuser")
	lv.passwordEntry.SetText("password123")
	lv.confirmPassword.SetText("different456")
	lv.handleRegister()

	assert.Contains(t, lv.Error(), "Passwords do not match")
}

func TestRegisterPasswordTooShort(t *testing.T) {
	lv, _ := setupLoginView(t)

	lv.toggleMode()

	lv.usernameEntry.SetText("newuser")
	lv.passwordEntry.SetText("short")
	lv.confirmPassword.SetText("short")
	lv.handleRegister()

	assert.Contains(t, lv.Error(), "must be at least 8 characters")
}

func TestRegisterSuccess(t *testing.T) {
	lv, _ := setupLoginView(t)

	lv.toggleMode()

	lv.usernameEntry.SetText("dave")
	lv.passwordEntry.SetText("securepass999")
	lv.confirmPassword.SetText("securepass999")

	var capturedUser *domain.User
	var wg sync.WaitGroup
	wg.Add(1)
	lv.OnLoginSuccess = func(user *domain.User) {
		capturedUser = user
		wg.Done()
	}

	lv.handleRegister()

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		assert.NotNil(t, capturedUser, "OnLoginSuccess must be called with a user")
		assert.Equal(t, "dave", capturedUser.Username)
	case <-time.After(5 * time.Second):
		t.Fatal("register did not complete within timeout")
	}
}

func TestSwitchToRegister(t *testing.T) {
	lv, _ := setupLoginView(t)

	assert.False(t, lv.IsRegister(), "initial mode must be login")
	assert.Equal(t, "Create Account", lv.switchLink.Text)

	lv.toggleMode()

	assert.True(t, lv.IsRegister(), "must be in register mode after toggle")
	assert.Equal(t, "Back to Login", lv.switchLink.Text)
	assert.False(t, lv.loginBtn.Visible(), "login button must be hidden in register mode")
	assert.True(t, lv.registerBtn.Visible(), "register button must be visible in register mode")
	assert.True(t, lv.confirmPassword.Visible(), "confirm password must be visible in register mode")

	lv.toggleMode()

	assert.False(t, lv.IsRegister(), "must be back in login mode after second toggle")
	assert.Equal(t, "Create Account", lv.switchLink.Text)
	assert.True(t, lv.loginBtn.Visible(), "login button must be visible after switching back")
	assert.False(t, lv.registerBtn.Visible(), "register button must be hidden after switching back")
}

func TestLoadingDisablesForm(t *testing.T) {
	lv, _ := setupLoginView(t)

	lv.setLoading(true)

	assert.True(t, lv.Loading(), "loading must be true")
	assert.True(t, lv.loginBtn.Disabled(), "login button must be disabled while loading")
	assert.True(t, lv.registerBtn.Disabled(), "register button must be disabled while loading")
	assert.True(t, lv.usernameEntry.Disabled(), "username entry must be disabled while loading")
	assert.True(t, lv.passwordEntry.Disabled(), "password entry must be disabled while loading")
	assert.True(t, lv.confirmPassword.Disabled(), "confirm password entry must be disabled while loading")
	assert.True(t, lv.progress.Visible(), "progress bar must be visible while loading")

	lv.setLoading(false)

	assert.False(t, lv.Loading(), "loading must be false after stop")
	assert.False(t, lv.loginBtn.Disabled(), "login button must be enabled after loading stops")
	assert.False(t, lv.usernameEntry.Disabled(), "username entry must be enabled after loading stops")
	assert.False(t, lv.passwordEntry.Disabled(), "password entry must be enabled after loading stops")
	assert.False(t, lv.progress.Visible(), "progress bar must be hidden after loading stops")
}

func TestDoubleSubmitGuarded(t *testing.T) {
	lv, authSvc := setupLoginView(t)

	_, err := authSvc.Register(context.Background(), "guard", "mypassword123")
	require.NoError(t, err)

	lv.usernameEntry.SetText("guard")
	lv.passwordEntry.SetText("mypassword123")

	callCount := 0
	var mu sync.Mutex
	lv.OnLoginSuccess = func(_ *domain.User) {
		mu.Lock()
		callCount++
		mu.Unlock()
	}

	lv.setLoading(true)
	lv.handleLogin()
	lv.setLoading(false)

	assert.LessOrEqual(t, callCount, 1, "OnLoginSuccess must not be called more than once")
}

func TestClearErrorsOnModeSwitch(t *testing.T) {
	lv, _ := setupLoginView(t)

	lv.setError("Some error")
	assert.NotEmpty(t, lv.Error())

	lv.toggleMode()

	assert.Empty(t, lv.Error(), "errors must be cleared on mode switch")
}

func TestContentIsCanvasObject(t *testing.T) {
	lv, _ := setupLoginView(t)

	content := lv.Content()
	assert.NotNil(t, content, "Content() must not return nil")
	assert.Implements(t, (*interface{ Visible() bool })(nil), content,
		"Content() must return a CanvasObject")
}
