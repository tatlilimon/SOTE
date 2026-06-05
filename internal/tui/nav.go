package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	lip "charm.land/lipgloss/v2"
)

type viewState int

const (
	loginView viewState = iota
	registerView
	mainMenuView
	chatView
	contactsView
	settingsView
)

func (v viewState) String() string {
	switch v {
	case loginView:
		return "login"
	case registerView:
		return "register"
	case mainMenuView:
		return "mainMenu"
	case chatView:
		return "chat"
	case contactsView:
		return "contacts"
	case settingsView:
		return "settings"
	default:
		return "unknown"
	}
}

type menuItem struct {
	label    string
	shortcut string
	view     viewState
	action   string
}

var menuItems = []menuItem{
	{label: "Send Message", shortcut: "1", view: chatView, action: "view"},
	{label: "Contacts", shortcut: "2", view: contactsView, action: "view"},
	{label: "Show Onion Address", shortcut: "3", view: mainMenuView, action: "toggle"},
	{label: "Settings", shortcut: "4", view: settingsView, action: "view"},
	{label: "Exit", shortcut: "5", view: mainMenuView, action: "quit"},
}

type NavModel struct {
	activeView viewState
	width      int
	height     int

	menuCursor int

	username  string
	torStatus string
	connected bool

	showOnion bool
	onionAddr string

	errMsg string
}

func NewNavModel() NavModel {
	return NavModel{
		activeView: mainMenuView,
		width:      MinTerminalWidth,
		height:     MinTerminalHeight,
		torStatus:  "disconnected",
		connected:  false,
	}
}

func (m NavModel) Init() tea.Cmd {
	return nil
}

func (m NavModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyPressMsg:
		key := msg.String()

		switch key {
		case "ctrl+c", "ctrl+d":
			return m, tea.Quit
		case "q":
			if m.activeView != mainMenuView {
				m.activeView = mainMenuView
				m.menuCursor = 0
				m.errMsg = ""
				return m, nil
			}
			return m, tea.Quit
		case "esc":
			if m.activeView != mainMenuView {
				m.activeView = mainMenuView
				m.menuCursor = 0
				m.errMsg = ""
				return m, nil
			}
			return m, nil
		}

		switch m.activeView {
		case mainMenuView:
			return m.handleMainMenu(msg)
		case chatView:
			return m.handleChat(msg)
		case contactsView:
			return m.handleContacts(msg)
		case settingsView:
			return m.handleSettings(msg)
		case loginView:
			return m.handleLogin(msg)
		case registerView:
			return m.handleRegister(msg)
		}
	}

	return m, nil
}

func (m NavModel) View() tea.View {
	header := m.renderHeader()
	content := m.renderContent()
	statusBar := m.renderStatusBar()

	composed := lip.JoinVertical(lip.Top,
		header,
		content,
		statusBar,
	)

	return tea.NewView(composed)
}

func (m NavModel) handleMainMenu(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.menuCursor > 0 {
			m.menuCursor--
		}
	case "down", "j":
		if m.menuCursor < len(menuItems)-1 {
			m.menuCursor++
		}
	case "enter":
		return m.executeMenuAction()
	case "1", "2", "3", "4", "5":
		idx := int(msg.String()[0] - '1')
		if idx >= 0 && idx < len(menuItems) {
			m.menuCursor = idx
			return m.executeMenuAction()
		}
	}

	return m, nil
}

func (m NavModel) executeMenuAction() (tea.Model, tea.Cmd) {
	if m.menuCursor < 0 || m.menuCursor >= len(menuItems) {
		return m, nil
	}

	item := menuItems[m.menuCursor]

	switch item.action {
	case "view":
		m.activeView = item.view
		m.errMsg = ""
		return m, nil
	case "toggle":
		m.showOnion = !m.showOnion
		m.errMsg = ""
		return m, nil
	case "quit":
		return m, tea.Quit
	}

	return m, nil
}

func (m NavModel) handleChat(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	return m, nil
}

func (m NavModel) handleContacts(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	return m, nil
}

func (m NavModel) handleSettings(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	return m, nil
}

func (m NavModel) handleLogin(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	return m, nil
}

func (m NavModel) handleRegister(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	return m, nil
}

func (m NavModel) renderHeader() string {
	title := "SOTE — Decentralized Messenger"
	return HeaderStyle.Width(m.width).Render(title)
}

func (m NavModel) renderStatusBar() string {
	var parts []string

	connIndicator := StatusDot(m.connected, m.torStatus == "connecting")
	parts = append(parts, connIndicator+" "+m.torStatus)

	if m.username != "" {
		parts = append(parts, "User: "+m.username)
	} else {
		parts = append(parts, "Not logged in")
	}

	if m.showOnion && m.onionAddr != "" {
		parts = append(parts, "Onion: "+m.onionAddr)
	} else if m.showOnion {
		parts = append(parts, "Onion: not available")
	}

	parts = append(parts, "["+m.activeView.String()+"]")

	left := strings.Join(parts, " | ")

	barStyle := StatusBarStyle.Width(m.width)
	return barStyle.Render(left)
}

func (m NavModel) renderContent() string {
	const headerLines = 1
	const statusLines = 1
	contentHeight := m.height - headerLines - statusLines
	if contentHeight < 1 {
		contentHeight = 1
	}
	contentWidth := m.width

	switch m.activeView {
	case mainMenuView:
		return m.renderMainMenu(contentWidth, contentHeight)
	case chatView:
		return m.renderChatPlaceholder(contentWidth, contentHeight)
	case contactsView:
		return m.renderContactsPlaceholder(contentWidth, contentHeight)
	case settingsView:
		return m.renderSettingsPlaceholder(contentWidth, contentHeight)
	case loginView:
		return m.renderLoginPlaceholder(contentWidth, contentHeight)
	case registerView:
		return m.renderRegisterPlaceholder(contentWidth, contentHeight)
	default:
		return m.renderMainMenu(contentWidth, contentHeight)
	}
}

func (m NavModel) renderMainMenu(width, height int) string {
	var sb strings.Builder

	sb.WriteString(MenuTitleStyle.Width(width).Render("Main Menu"))
	sb.WriteString("\n\n")

	for i, item := range menuItems {
		prefix := fmt.Sprintf("[%s] ", item.shortcut)
		if i == m.menuCursor {
			sb.WriteString(MenuItemSelectedStyle.Width(width - 2).Render(prefix + item.label))
		} else {
			sb.WriteString(MenuItemStyle.Width(width - 2).Render(prefix + item.label))
		}
		sb.WriteString("\n")
	}

	lineCount := len(menuItems) + 3
	if height > lineCount {
		padding := (height - lineCount) / 2
		for i := 0; i < padding; i++ {
			sb.WriteString("\n")
		}
	}

	sb.WriteString("\n")
	sb.WriteString(HelpStyle.Width(width).Render("↑↓ navigate · Enter select · 1-5 shortcut · q quit"))

	if m.errMsg != "" {
		sb.WriteString("\n")
		sb.WriteString(ErrorStyle.Width(width).Render(m.errMsg))
	}

	return BaseStyle.Width(width).Height(height).Render(sb.String())
}

func (m NavModel) renderChatPlaceholder(width, height int) string {
	return RenderEmptyState(width, "Chat view — coming soon")
}

func (m NavModel) renderContactsPlaceholder(width, height int) string {
	return RenderEmptyState(width, "Contacts view — coming soon")
}

func (m NavModel) renderSettingsPlaceholder(width, height int) string {
	return RenderEmptyState(width, "Settings view — coming soon")
}

func (m NavModel) renderLoginPlaceholder(width, height int) string {
	return RenderEmptyState(width, "Login view — coming soon")
}

func (m NavModel) renderRegisterPlaceholder(width, height int) string {
	return RenderEmptyState(width, "Register view — coming soon")
}

func (m *NavModel) SetUsername(name string) {
	m.username = name
}

func (m *NavModel) SetTorStatus(status string, connected bool) {
	m.torStatus = status
	m.connected = connected
}

func (m *NavModel) SetOnionAddr(addr string) {
	m.onionAddr = addr
}

func (m *NavModel) SetError(msg string) {
	m.errMsg = msg
}

func (m *NavModel) ClearError() {
	m.errMsg = ""
}

func (m NavModel) ActiveView() string {
	return m.activeView.String()
}

func (m *NavModel) NavigateTo(view string) {
	switch view {
	case "login":
		m.activeView = loginView
	case "register":
		m.activeView = registerView
	case "mainMenu":
		m.activeView = mainMenuView
	case "chat":
		m.activeView = chatView
	case "contacts":
		m.activeView = contactsView
	case "settings":
		m.activeView = settingsView
	}
	m.errMsg = ""
}
