//go:build !nogui

package gui

import (
	"fmt"
	"image/color"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"sote/internal/domain"
)

const (
	Version     = "SOTE_Alpha_v1.0"
	LicenseText = "GPLv3"
	AboutCredit = "Built with Fyne toolkit"
)

var ErrNoUserSession = fmt.Errorf("no active user session")

// PasswordChanger delegates password-change operations. Implementations
// must verify old credentials, hash the new password, and re-encrypt the
// user's private key.
type PasswordChanger interface {
	ChangePassword(username, currentPassword, newPassword string) error
}

// Settings renders application settings inside a widget.Accordion with four
// sections: Account, Password, Connection, About.
//
// Password changes run asynchronously in a separate goroutine and use
// fyne.Do to show dialogs on the UI thread.
type Settings struct {
	user    *domain.User
	changer PasswordChanger
	win     fyne.Window

	torOnline bool
	socksPort int
	uptime    string

	usernameLabel *widget.Label
	onionLabel    *widget.Label
	copyBtn       *widget.Button

	currentEntry *widget.Entry
	newEntry     *widget.Entry
	confirmEntry *widget.Entry
	changeBtn    *widget.Button
	passStatus   *widget.Label

	torStatusText *canvas.Text
	socksLabel    *widget.Label
	uptimeLabel   *widget.Label

	versionLabel *widget.Label
	licenseLabel *widget.Label
	creditLabel  *widget.Label

	accordion *widget.Accordion

	bridgesCheck *widget.Check
	bridgesEntry *widget.Entry
}

// SetBridges adds the Tor bridges section: a switch, the bridge lines and a
// save button. Changes apply the next time SOTE starts Tor.
func (s *Settings) SetBridges(use bool, text string, saver func(use bool, text string) error) {
	s.bridgesCheck = widget.NewCheck("Use bridges (for networks that block Tor)", nil)
	s.bridgesCheck.SetChecked(use)
	s.bridgesEntry = widget.NewMultiLineEntry()
	s.bridgesEntry.SetPlaceHolder("obfs4 192.0.2.10:443 FINGERPRINT cert=... iat-mode=0\nClientTransportPlugin obfs4 exec /usr/bin/lyrebird")
	s.bridgesEntry.SetText(text)
	s.bridgesEntry.SetMinRowsVisible(5)
	save := widget.NewButtonWithIcon("Save Bridges", theme.DocumentSaveIcon(), func() {
		if err := saver(s.bridgesCheck.Checked, s.bridgesEntry.Text); err != nil {
			dialog.ShowError(err, s.win)
			return
		}
		dialog.ShowInformation("Bridges Saved", "Restart SOTE to connect with the new bridge settings.", s.win)
	})
	help := widget.NewLabel("One bridge per line (from bridges.torproject.org). obfs4, snowflake and webtunnel bridges also need a ClientTransportPlugin line.")
	help.Wrapping = fyne.TextWrapWord
	s.accordion.Append(widget.NewAccordionItem("Tor Bridges", container.NewVBox(s.bridgesCheck, help, s.bridgesEntry, save)))
}

func NewSettings(win fyne.Window, user *domain.User, changer PasswordChanger) *Settings {
	s := &Settings{
		win:     win,
		user:    user,
		changer: changer,
	}
	s.build()
	return s
}

func (s *Settings) SetUser(user *domain.User) {
	s.user = user
	s.refreshAccount()
}

func (s *Settings) SetTorStatus(online bool, socksPort int, uptime string) {
	s.torOnline = online
	s.socksPort = socksPort
	s.uptime = uptime
	s.refreshConnection()
}

func (s *Settings) Content() fyne.CanvasObject {
	return s.accordion
}

func ExposesPrivateKey(view string) bool {
	upper := strings.ToUpper(view)
	for _, marker := range []string{"BEGIN PGP PRIVATE KEY", "PRIVATE KEY BLOCK"} {
		if strings.Contains(upper, marker) {
			return true
		}
	}
	return false
}

func (s *Settings) build() {
	s.accordion = widget.NewAccordion(
		widget.NewAccordionItem("Account", s.buildAccountSection()),
		widget.NewAccordionItem("Password", s.buildPasswordSection()),
		widget.NewAccordionItem("Connection", s.buildConnectionSection()),
		widget.NewAccordionItem("About", s.buildAboutSection()),
	)
}

func (s *Settings) buildAccountSection() fyne.CanvasObject {
	username := ""
	onion := "Not available"
	if s.user != nil {
		username = s.user.Username
		if s.user.OnionAddress != "" {
			onion = s.user.OnionAddress
		}
	}

	s.usernameLabel = widget.NewLabelWithStyle(
		fmt.Sprintf("Username: %s", username),
		fyne.TextAlignLeading,
		fyne.TextStyle{Bold: true},
	)

	s.onionLabel = widget.NewLabel(fmt.Sprintf("Onion Address: %s", onion))
	s.onionLabel.Wrapping = fyne.TextTruncate

	s.copyBtn = widget.NewButtonWithIcon("Copy", theme.ContentCopyIcon(), func() {
		// Read the address at click time: it is set after login.
		if s.user != nil && s.user.OnionAddress != "" {
			s.win.Clipboard().SetContent(s.user.OnionAddress)
		}
	})

	return container.NewVBox(s.usernameLabel, s.onionLabel, s.copyBtn)
}

func (s *Settings) refreshAccount() {
	if s.usernameLabel == nil {
		return
	}
	username := ""
	onion := "Not available"
	if s.user != nil {
		username = s.user.Username
		if s.user.OnionAddress != "" {
			onion = s.user.OnionAddress
		}
	}
	s.usernameLabel.SetText(fmt.Sprintf("Username: %s", username))
	s.onionLabel.SetText(fmt.Sprintf("Onion Address: %s", onion))
}

func (s *Settings) buildPasswordSection() fyne.CanvasObject {
	s.currentEntry = widget.NewEntry()
	s.currentEntry.PlaceHolder = "Current password"
	s.currentEntry.Password = true

	s.newEntry = widget.NewEntry()
	s.newEntry.PlaceHolder = "New password (min 8 chars)"
	s.newEntry.Password = true

	s.confirmEntry = widget.NewEntry()
	s.confirmEntry.PlaceHolder = "Confirm new password"
	s.confirmEntry.Password = true

	s.passStatus = widget.NewLabel("")
	s.passStatus.Wrapping = fyne.TextWrapWord

	s.changeBtn = widget.NewButton("Change Password", s.handlePasswordChange)
	s.changeBtn.Importance = widget.HighImportance

	return container.NewVBox(
		s.currentEntry, s.newEntry, s.confirmEntry, s.passStatus, s.changeBtn,
	)
}

func (s *Settings) handlePasswordChange() {
	current := strings.TrimSpace(s.currentEntry.Text)
	newPass := strings.TrimSpace(s.newEntry.Text)
	confirm := strings.TrimSpace(s.confirmEntry.Text)

	s.passStatus.SetText("")

	if current == "" || newPass == "" || confirm == "" {
		s.passStatus.SetText("All fields are required.")
		return
	}
	if len(newPass) < 8 {
		s.passStatus.SetText("New password must be at least 8 characters.")
		return
	}
	if newPass != confirm {
		s.passStatus.SetText("Passwords do not match.")
		return
	}
	if current == newPass {
		s.passStatus.SetText("New password must differ from current password.")
		return
	}
	if s.user == nil {
		s.passStatus.SetText(ErrNoUserSession.Error())
		return
	}

	s.changeBtn.Disable()
	s.passStatus.SetText("Changing password...")

	username := s.user.Username
	go func() {
		err := s.changer.ChangePassword(username, current, newPass)
		fyne.Do(func() {
			s.changeBtn.Enable()
			s.currentEntry.SetText("")
			s.newEntry.SetText("")
			s.confirmEntry.SetText("")
			s.passStatus.SetText("")

			if err != nil {
				dialog.ShowError(err, s.win)
			} else {
				if s.user != nil {
					s.user.RawPassword = newPass
				}
				dialog.ShowInformation("Password Changed",
					"Password changed successfully.", s.win)
			}
		})
	}()
}

func (s *Settings) buildConnectionSection() fyne.CanvasObject {
	s.torStatusText = canvas.NewText("", color.White)
	s.torStatusText.TextSize = theme.TextSize()
	s.torStatusText.TextStyle = fyne.TextStyle{Bold: true}

	s.socksLabel = widget.NewLabel("")
	s.uptimeLabel = widget.NewLabel("")

	s.refreshConnection()

	return container.NewVBox(s.torStatusText, s.socksLabel, s.uptimeLabel)
}

func (s *Settings) refreshConnection() {
	if s.torStatusText == nil {
		return
	}

	if s.torOnline {
		s.torStatusText.Text = "Tor Status: Connected"
		s.torStatusText.Color = color.NRGBA{R: 0x10, G: 0xB9, B: 0x81, A: 0xFF}
	} else {
		s.torStatusText.Text = "Tor Status: Disconnected"
		s.torStatusText.Color = color.NRGBA{R: 0xEF, G: 0x44, B: 0x44, A: 0xFF}
	}
	s.torStatusText.Refresh()

	if s.socksPort > 0 {
		s.socksLabel.SetText(fmt.Sprintf("SOCKS Port: %d", s.socksPort))
	} else {
		s.socksLabel.SetText("SOCKS Port: N/A")
	}

	uptime := s.uptime
	if uptime == "" {
		uptime = "N/A"
	}
	s.uptimeLabel.SetText(fmt.Sprintf("Uptime: %s", uptime))
}

func (s *Settings) buildAboutSection() fyne.CanvasObject {
	s.versionLabel = widget.NewLabel(fmt.Sprintf("Version: %s", Version))
	s.licenseLabel = widget.NewLabel(fmt.Sprintf("License: %s", LicenseText))
	s.creditLabel = widget.NewLabel(AboutCredit)

	return container.NewVBox(s.versionLabel, s.licenseLabel, s.creditLabel)
}
