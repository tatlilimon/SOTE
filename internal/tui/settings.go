package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"sote/internal/domain"
)

const Version = "SOTE_Alpha_v1.0"
const LicenseText = "GPLv3"

type PasswordChanger interface {
	ChangePassword(username, currentPassword, newPassword string) error
}

type PasswordChangedMsg struct {
	Error error
}

type SettingsModel struct {
	width  int
	height int

	user *domain.User

	torOnline bool
	uptime    string
	socksPort int

	currentInput textinput.Model
	newInput     textinput.Model
	confirmInput textinput.Model

	focusIdx int

	errorMsg   string
	successMsg string

	showPasswordForm bool
	submitting       bool

	passwordChanger PasswordChanger
	focused         bool
}

func NewSettingsModel(user *domain.User, changer PasswordChanger) SettingsModel {
	currentInput := textinput.New()
	currentInput.Placeholder = "Current password"
	currentInput.EchoMode = textinput.EchoPassword
	currentInput.EchoCharacter = '•'
	currentInput.CharLimit = 128
	currentInput.SetWidth(40)
	currentInput.Prompt = "Current:  "

	newInput := textinput.New()
	newInput.Placeholder = "New password (min 8 chars)"
	newInput.EchoMode = textinput.EchoPassword
	newInput.EchoCharacter = '•'
	newInput.CharLimit = 128
	newInput.SetWidth(40)
	newInput.Prompt = "New:      "

	confirmInput := textinput.New()
	confirmInput.Placeholder = "Confirm new password"
	confirmInput.EchoMode = textinput.EchoPassword
	confirmInput.EchoCharacter = '•'
	confirmInput.CharLimit = 128
	confirmInput.SetWidth(40)
	confirmInput.Prompt = "Confirm:  "

	return SettingsModel{
		user:            user,
		passwordChanger: changer,
		currentInput:    currentInput,
		newInput:        newInput,
		confirmInput:    confirmInput,
		focusIdx:        0,
	}
}

func (m *SettingsModel) SetTorStatus(online bool, socksPort int, uptime string) {
	m.torOnline = online
	m.socksPort = socksPort
	m.uptime = uptime
}

func (m SettingsModel) Init() tea.Cmd {
	if m.showPasswordForm {
		return m.currentInput.Focus()
	}
	return nil
}

func (m SettingsModel) Update(msg tea.Msg) (SettingsModel, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKey(msg)
	case PasswordChangedMsg:
		return m.handlePasswordChanged(msg)
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	}

	if m.showPasswordForm {
		var cmd tea.Cmd
		switch m.focusIdx {
		case 0:
			m.currentInput, cmd = m.currentInput.Update(msg)
		case 1:
			m.newInput, cmd = m.newInput.Update(msg)
		case 2:
			m.confirmInput, cmd = m.confirmInput.Update(msg)
		}
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	return m, tea.Batch(cmds...)
}

func (m SettingsModel) handleKey(msg tea.KeyMsg) (SettingsModel, tea.Cmd) {
	if m.showPasswordForm {
		switch msg.String() {
		case "tab":
			return m.focusNext()
		case "shift+tab":
			return m.focusPrev()
		case "enter":
			return m.submitPasswordChange()
		case "esc":
			return m.closePasswordForm()
		}
	}

	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	}

	return m, nil
}

func (m SettingsModel) focusNext() (SettingsModel, tea.Cmd) {
	m.focusIdx = (m.focusIdx + 1) % 3
	return m, m.focusField(m.focusIdx)
}

func (m SettingsModel) focusPrev() (SettingsModel, tea.Cmd) {
	m.focusIdx--
	if m.focusIdx < 0 {
		m.focusIdx = 2
	}
	return m, m.focusField(m.focusIdx)
}

func (m SettingsModel) focusField(idx int) tea.Cmd {
	m.currentInput.Blur()
	m.newInput.Blur()
	m.confirmInput.Blur()
	switch idx {
	case 0:
		return m.currentInput.Focus()
	case 1:
		return m.newInput.Focus()
	default:
		return m.confirmInput.Focus()
	}
}

func (m SettingsModel) closePasswordForm() (SettingsModel, tea.Cmd) {
	m.showPasswordForm = false
	m.currentInput.Blur()
	m.newInput.Blur()
	m.confirmInput.Blur()
	m.currentInput.SetValue("")
	m.newInput.SetValue("")
	m.confirmInput.SetValue("")
	m.errorMsg = ""
	m.successMsg = ""
	m.submitting = false
	return m, nil
}

func (m SettingsModel) submitPasswordChange() (SettingsModel, tea.Cmd) {
	m.errorMsg = ""
	m.successMsg = ""

	current := m.currentInput.Value()
	newPass := m.newInput.Value()
	confirm := m.confirmInput.Value()

	if current == "" || newPass == "" || confirm == "" {
		m.errorMsg = "All fields are required."
		return m, nil
	}
	if len(newPass) < 8 {
		m.errorMsg = "New password must be at least 8 characters."
		return m, nil
	}
	if newPass != confirm {
		m.errorMsg = "Passwords do not match."
		return m, nil
	}
	if current == newPass {
		m.errorMsg = "New password must differ from current password."
		return m, nil
	}

	m.submitting = true
	return m, m.doChangePassword(current, newPass)
}

func (m SettingsModel) doChangePassword(current, newPass string) tea.Cmd {
	return func() tea.Msg {
		err := m.passwordChanger.ChangePassword(m.user.Username, current, newPass)
		return PasswordChangedMsg{Error: err}
	}
}

func (m SettingsModel) handlePasswordChanged(msg PasswordChangedMsg) (SettingsModel, tea.Cmd) {
	m.submitting = false

	if msg.Error != nil {
		m.errorMsg = msg.Error.Error()
		return m, nil
	}

	m.successMsg = "Password changed successfully."
	m.currentInput.SetValue("")
	m.newInput.SetValue("")
	m.confirmInput.SetValue("")
	m.errorMsg = ""
	m.showPasswordForm = false

	if m.user != nil {
		m.user.RawPassword = m.newInput.Value()
	}

	return m, nil
}

func (m SettingsModel) View() string {
	if m.width == 0 {
		m.width = 80
	}

	var sections []string
	sections = append(sections, m.renderHeader())
	sections = append(sections, m.renderUserInfo())
	sections = append(sections, m.renderPasswordSection())
	sections = append(sections, m.renderConnectionInfo())
	sections = append(sections, m.renderAbout())

	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

func (m SettingsModel) renderHeader() string {
	return HeaderStyle.Width(m.width).Render(" Settings ")
}

func (m SettingsModel) renderUserInfo() string {
	username := ""
	onion := "Not available"

	if m.user != nil {
		username = m.user.Username
		if m.user.OnionAddress != "" {
			onion = m.user.OnionAddress
		}
	}

	lines := []string{
		SectionTitleStyle.Render("User Information"),
		renderInfoRow("Username:", username),
		renderInfoRow("Onion Address:", onion),
	}

	return CardStyle.Width(m.width - 4).Render(
		lipgloss.JoinVertical(lipgloss.Left, lines...),
	)
}

func (m SettingsModel) renderPasswordSection() string {
	if !m.showPasswordForm {
		button := ButtonStylePrimary.Render(" Change Password ")
		hint := MutedStyle.Render(" (press Enter to expand)")

		return CardStyle.Width(m.width - 4).Render(
			lipgloss.JoinVertical(lipgloss.Left,
				SectionTitleStyle.Render("Password"),
				lipgloss.JoinHorizontal(lipgloss.Center, button, hint),
			),
		)
	}

	var lines []string
	lines = append(lines, SectionTitleStyle.Render("Change Password"))

	if m.errorMsg != "" {
		lines = append(lines, ErrorStyle.Render("  ✗ "+m.errorMsg))
	}
	if m.successMsg != "" {
		lines = append(lines, SuccessStyle.Render("  ✓ "+m.successMsg))
	}

	lines = append(lines, m.currentInput.View())
	lines = append(lines, m.newInput.View())
	lines = append(lines, m.confirmInput.View())

	help := HelpStyle.Render("Tab: next field \u2022 Shift+Tab: prev \u2022 Enter: submit \u2022 Esc: cancel")
	lines = append(lines, help)

	if m.submitting {
		lines = append(lines, MutedStyle.Italic(true).Render("  Verifying and updating..."))
	}

	return CardStyle.Width(m.width - 4).Render(
		lipgloss.JoinVertical(lipgloss.Left, lines...),
	)
}

func (m SettingsModel) renderConnectionInfo() string {
	torStatus := "Disconnected"
	statusStyle := ErrorStyle
	if m.torOnline {
		torStatus = "Connected"
		statusStyle = SuccessStyle
	}

	onion := "Not available"
	if m.user != nil && m.user.OnionAddress != "" {
		onion = m.user.OnionAddress
	}

	uptime := m.uptime
	if uptime == "" {
		uptime = "N/A"
	}

	lines := []string{
		SectionTitleStyle.Render("Connection"),
		renderInfoRow("Tor Status:", statusStyle.Render(torStatus)),
		renderInfoRow("Hidden Service:", onion),
		renderInfoRow("Uptime:", uptime),
	}
	if m.socksPort > 0 {
		lines = append(lines, renderInfoRow("SOCKS Port:", fmt.Sprintf("%d", m.socksPort)))
	}

	return CardStyle.Width(m.width - 4).Render(
		lipgloss.JoinVertical(lipgloss.Left, lines...),
	)
}

func (m SettingsModel) renderAbout() string {
	lines := []string{
		SectionTitleStyle.Render("About"),
		renderInfoRow("Version:", Version),
		renderInfoRow("License:", LicenseText),
		renderInfoRow("", "SOTE \u2014 Self-hosted Onion-encrypted Trustless Endpoint"),
	}

	return CardStyle.Width(m.width - 4).Render(
		lipgloss.JoinVertical(lipgloss.Left, lines...),
	)
}

func renderInfoRow(label, value string) string {
	lbl := LabelStyle.Render(label)
	val := ValueStyle.Render(value)
	return lipgloss.JoinHorizontal(lipgloss.Left, lbl, " ", val)
}

func (m *SettingsModel) OpenPasswordForm() tea.Cmd {
	m.showPasswordForm = true
	m.errorMsg = ""
	m.successMsg = ""
	m.currentInput.SetValue("")
	m.newInput.SetValue("")
	m.confirmInput.SetValue("")
	m.focusIdx = 0
	return m.currentInput.Focus()
}

func (m *SettingsModel) Focus() tea.Cmd {
	m.focused = true
	if m.showPasswordForm {
		return m.focusField(m.focusIdx)
	}
	return nil
}

func (m *SettingsModel) Blur() {
	m.focused = false
	if m.showPasswordForm {
		m.currentInput.Blur()
		m.newInput.Blur()
		m.confirmInput.Blur()
	}
}

func ExposesPrivateKey(view string) bool {
	upper := strings.ToUpper(view)
	markers := []string{
		"BEGIN PGP PRIVATE KEY",
		"PRIVATE KEY BLOCK",
	}
	for _, marker := range markers {
		if strings.Contains(upper, marker) {
			return true
		}
	}
	return false
}
