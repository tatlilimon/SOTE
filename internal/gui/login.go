package gui

import (
	"context"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"sote/internal/auth"
	"sote/internal/domain"
)

type LoginView struct {
	window  fyne.Window
	authSvc *auth.AuthService

	usernameEntry   *widget.Entry
	passwordEntry   *widget.Entry
	confirmPassword *widget.Entry

	loginBtn    *widget.Button
	registerBtn *widget.Button

	switchLink *widget.Hyperlink
	isRegister bool

	errorLabel  *widget.Label
	progress    *widget.ProgressBarInfinite
	statusLabel *widget.Label

	OnLoginSuccess func(user *domain.User)

	mu      sync.Mutex
	loading bool
}

// NewLoginView creates a LoginView bound to the given window and auth service.
// The OnLoginSuccess callback must be set by the caller before showing the view.
func NewLoginView(win fyne.Window, authSvc *auth.AuthService) *LoginView {
	lv := &LoginView{
		window: win,
		authSvc: authSvc,
	}
	lv.buildUI()
	return lv
}

// Content returns the root fyne.CanvasObject for this view.
func (lv *LoginView) Content() fyne.CanvasObject {
	return lv.container()
}

func (lv *LoginView) buildUI() {
	lv.usernameEntry = widget.NewEntry()
	lv.usernameEntry.SetPlaceHolder("Username")

	lv.passwordEntry = widget.NewPasswordEntry()
	lv.passwordEntry.SetPlaceHolder("Password")

	lv.confirmPassword = widget.NewPasswordEntry()
	lv.confirmPassword.SetPlaceHolder("Confirm Password")
	lv.confirmPassword.Hide()

	lv.errorLabel = widget.NewLabel("")
	lv.errorLabel.Importance = widget.DangerImportance
	lv.errorLabel.Hide()

	lv.statusLabel = widget.NewLabel("")
	lv.statusLabel.Hide()

	lv.loginBtn = widget.NewButton("Login", lv.handleLogin)
	lv.loginBtn.Importance = widget.HighImportance

	lv.registerBtn = widget.NewButton("Create Account", lv.handleRegister)
	lv.registerBtn.Importance = widget.HighImportance
	lv.registerBtn.Hide()

	lv.progress = widget.NewProgressBarInfinite()
	lv.progress.Hide()

	lv.switchLink = widget.NewHyperlink("Create Account", nil)
	lv.switchLink.OnTapped = lv.toggleMode

	lv.passwordEntry.OnSubmitted = func(_ string) {
		if lv.isRegister {
			lv.handleRegister()
		} else {
			lv.handleLogin()
		}
	}

	lv.usernameEntry.OnSubmitted = func(_ string) {
		lv.window.Canvas().Focus(lv.passwordEntry)
	}
	lv.confirmPassword.OnSubmitted = func(_ string) {
		lv.handleRegister()
	}
}

func (lv *LoginView) container() fyne.CanvasObject {
	titleLabel := widget.NewLabelWithStyle("SOTE", fyne.TextAlignCenter,
		fyne.TextStyle{Bold: true})
	subtitleLabel := widget.NewLabelWithStyle("Decentralized Messenger", fyne.TextAlignCenter,
		fyne.TextStyle{Italic: true})

	formTitle := widget.NewLabelWithStyle("Sign In", fyne.TextAlignCenter,
		fyne.TextStyle{Bold: true})

	content := container.NewVBox(
		titleLabel,
		subtitleLabel,
		widget.NewSeparator(),
		formTitle,
		widget.NewLabel("Username"),
		lv.usernameEntry,
		widget.NewLabel("Password"),
		lv.passwordEntry,
		lv.confirmPassword,
		lv.switchLink,
		lv.loginBtn,
		lv.registerBtn,
		lv.errorLabel,
		lv.statusLabel,
		lv.progress,
	)

	return container.NewPadded(container.NewCenter(container.NewVBox(
		container.NewPadded(content),
	)))
}
func (lv *LoginView) toggleMode() {
	lv.isRegister = !lv.isRegister
	lv.clearErrors()

	if lv.isRegister {
		lv.switchLink.SetText("Back to Login")
		lv.loginBtn.Hide()
		lv.registerBtn.Show()
		lv.confirmPassword.Show()
	} else {
		lv.switchLink.SetText("Create Account")
		lv.registerBtn.Hide()
		lv.loginBtn.Show()
		lv.confirmPassword.Hide()
	}
	lv.window.Content().Refresh()
}

func (lv *LoginView) handleLogin() {
	lv.mu.Lock()
	if lv.loading {
		lv.mu.Unlock()
		return
	}
	lv.mu.Unlock()

	username := lv.usernameEntry.Text
	password := lv.passwordEntry.Text

	if !lv.validateLoginFields(username, password) {
		return
	}

	lv.setLoading(true)

	go func() {
		user, err := lv.authSvc.Login(context.Background(), username, password)
		fyne.Do(func() {
			lv.setLoading(false)
			if err != nil {
				lv.setError(err.Error())
				return
			}
			if lv.OnLoginSuccess != nil {
				lv.OnLoginSuccess(user)
			}
		})
	}()
}

func (lv *LoginView) handleRegister() {
	lv.mu.Lock()
	if lv.loading {
		lv.mu.Unlock()
		return
	}
	lv.mu.Unlock()

	username := lv.usernameEntry.Text
	password := lv.passwordEntry.Text
	confirm := lv.confirmPassword.Text

	if !lv.validateRegisterFields(username, password, confirm) {
		return
	}

	lv.setLoading(true)

	go func() {
		user, err := lv.authSvc.Register(context.Background(), username, password)
		fyne.Do(func() {
			lv.setLoading(false)
			if err != nil {
				lv.setError(err.Error())
				return
			}
			if lv.OnLoginSuccess != nil {
				lv.OnLoginSuccess(user)
			}
		})
	}()
}

func (lv *LoginView) validateLoginFields(username, password string) bool {
	lv.clearErrors()

	if username == "" {
		lv.setError("Username is required")
		return false
	}
	if password == "" {
		lv.setError("Password is required")
		return false
	}
	return true
}

func (lv *LoginView) validateRegisterFields(username, password, confirm string) bool {
	lv.clearErrors()

	if username == "" {
		lv.setError("Username is required")
		return false
	}
	if password == "" {
		lv.setError("Password is required")
		return false
	}
	if len(password) < 8 {
		lv.setError("Password must be at least 8 characters")
		return false
	}
	if confirm == "" {
		lv.setError("Please confirm your password")
		return false
	}
	if password != confirm {
		lv.setError("Passwords do not match")
		return false
	}
	return true
}

func (lv *LoginView) setLoading(loading bool) {
	lv.mu.Lock()
	lv.loading = loading
	lv.mu.Unlock()

	if loading {
		lv.progress.Show()
		lv.progress.Start()
		lv.loginBtn.Disable()
		lv.registerBtn.Disable()
		lv.usernameEntry.Disable()
		lv.passwordEntry.Disable()
		lv.confirmPassword.Disable()
	} else {
		lv.progress.Hide()
		lv.progress.Stop()
		lv.loginBtn.Enable()
		lv.registerBtn.Enable()
		lv.usernameEntry.Enable()
		lv.passwordEntry.Enable()
		lv.confirmPassword.Enable()
	}
}

func (lv *LoginView) setError(msg string) {
	lv.errorLabel.SetText(msg)
	lv.errorLabel.Show()
	lv.statusLabel.SetText(msg)
	lv.statusLabel.Show()
}

func (lv *LoginView) clearErrors() {
	lv.errorLabel.SetText("")
	lv.errorLabel.Hide()
	lv.statusLabel.SetText("")
	lv.statusLabel.Hide()
}

// IsRegister reports whether the view is currently in register mode.
func (lv *LoginView) IsRegister() bool { return lv.isRegister }

// Loading reports whether an auth operation is in progress.
func (lv *LoginView) Loading() bool {
	lv.mu.Lock()
	defer lv.mu.Unlock()
	return lv.loading
}

// Error returns the current error message, or "" if none.
func (lv *LoginView) Error() string { return lv.errorLabel.Text }
