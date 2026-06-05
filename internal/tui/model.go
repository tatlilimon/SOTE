package tui

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	tea "charm.land/bubbletea/v2"
	lip "charm.land/lipgloss/v2"

	"sote/internal/auth"
	"sote/internal/config"
	"sote/internal/contacts"
	"sote/internal/crypto"
	"sote/internal/domain"
	"sote/internal/messages"
	"sote/internal/storage"
	"sote/internal/tor"
	"sote/internal/transport"
)

type torStartedMsg struct {
	addr string
	err  error
}

type torStatusMsg struct {
	online bool
	status string
}

type incomingMsgMsg struct {
	sender  string
	content []byte
	err     error
}

type contactReqMsg struct {
	fromUser      string
	fromOnion     string
	fromPublicKey []byte
}

type shutdownMsg struct{}

type passwordChanger struct {
	db     *sql.DB
	crypto *crypto.Service
}

func (pc *passwordChanger) ChangePassword(username, currentPassword, newPassword string) error {
	su, err := storage.GetUser(pc.db, username)
	if err != nil {
		return fmt.Errorf("get user: %w", err)
	}
	if err := pc.crypto.VerifyPassword(su.Password, currentPassword); err != nil {
		return fmt.Errorf("current password is incorrect")
	}
	newHash, err := pc.crypto.HashPassword(newPassword)
	if err != nil {
		return fmt.Errorf("hash new password: %w", err)
	}
	privKey, err := pc.crypto.DecryptAES(su.PrivateKey, currentPassword)
	if err != nil {
		return fmt.Errorf("decrypt private key: %w", err)
	}
	newEncKey, err := pc.crypto.EncryptAES(privKey, newPassword)
	if err != nil {
		return fmt.Errorf("re-encrypt private key: %w", err)
	}
	if err := storage.SaveUser(pc.db, username, newHash, newEncKey, su.PublicKey, su.OnionAddress, su.TorrcFilePath); err != nil {
		return fmt.Errorf("save user: %w", err)
	}
	return nil
}

type Model struct {
	cfg       *config.Config
	db        *sql.DB
	authSvc   *auth.AuthService
	cryptoSvc *crypto.Service
	contactSvc *contacts.Service
	msgSvc    *messages.Service
	torSvc    *tor.TorService
	server    *transport.Server
	client    *transport.Client

	activeView viewState
	width      int
	height     int

	login     LoginModel
	register  RegisterModel
	chat      *ChatModel
	contactsT *ContactsModel
	settings  SettingsModel

	currentUser  *domain.User
	torOnline    bool
	onionAddr    string
	errMsg       string
	quitting     bool
	shuttingDown bool
}

func NewModel(cfg *config.Config, db *sql.DB,
	authSvc *auth.AuthService, cryptoSvc *crypto.Service,
	contactSvc *contacts.Service, msgSvc *messages.Service,
	torSvc *tor.TorService, srv *transport.Server, client *transport.Client,
) Model {
	pc := &passwordChanger{db: db, crypto: cryptoSvc}

	loginM := NewLoginModel(func(username, password string) (*domain.User, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		return authSvc.Login(ctx, username, password)
	})

	registerM := NewRegisterModel(func(username, password string) (*domain.User, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		return authSvc.Register(ctx, username, password)
	})

	contactsM := NewContactsModel("")
	contactsM.SetSize(80, MinTerminalHeight)

	settingsM := NewSettingsModel(nil, pc)

	return Model{
		cfg:        cfg,
		db:         db,
		authSvc:    authSvc,
		cryptoSvc:  cryptoSvc,
		contactSvc: contactSvc,
		msgSvc:     msgSvc,
		torSvc:     torSvc,
		server:     srv,
		client:     client,

		activeView: loginView,
		width:      MinTerminalWidth,
		height:     MinTerminalHeight,

		login:     loginM,
		register:  registerM,
		contactsT: &contactsM,
		settings:  settingsM,
	}
}

func (m Model) Init() tea.Cmd {
	m.login.SetSize(m.width, m.height)
	m.register.SetSize(m.width, m.height)
	return m.startTorCmd()
}

func (m *Model) startTorCmd() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		if err := m.torSvc.Start(ctx); err != nil {
			return torStartedMsg{err: err}
		}
		addr := m.torSvc.GetOnionAddress()
		return torStartedMsg{addr: addr}
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.handleResize(msg.Width, msg.Height)
		return m, nil

	case torStartedMsg:
		if msg.err != nil {
			m.errMsg = fmt.Sprintf("Tor: %v", msg.err)
		} else {
			m.onionAddr = msg.addr
			m.torOnline = true
			slog.Info("tor started", "onion", msg.addr)
		}
		return m, nil

	case torStatusMsg:
		m.torOnline = msg.online
		return m, nil

	case incomingMsgMsg:
		if msg.err != nil {
			m.errMsg = fmt.Sprintf("incoming message: %v", msg.err)
			return m, nil
		}
		if m.activeView == chatView && m.currentUser != nil && m.chat != nil {
			if msg.sender == m.chat.ContactName() {
				cmds = append(cmds, m.chat.LoadMessagesCmd())
			}
		}
		return m, tea.Batch(cmds...)

	case contactReqMsg:
		item := contactItem{
			username:  msg.fromUser,
			onionAddr: msg.fromOnion,
			pending:   true,
		}
		if m.contactsT != nil {
			m.contactsT.AppendPending(item)
		}
		return m, nil

	case shutdownMsg:
		m.shuttingDown = true
		return m, tea.Sequence(m.shutdownCmd(), tea.Quit)

	case tea.KeyPressMsg:
		return m.handleKeyPress(msg)
	}

	cmd := m.dispatchToActiveView(msg)
	if cmd != nil {
		cmds = append(cmds, cmd)
	}
	m.checkTransitions()
	return m, tea.Batch(cmds...)
}

func (m Model) handleKeyPress(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	if key == "ctrl+c" || key == "ctrl+d" {
		if m.activeView == loginView || m.activeView == registerView {
			_, cmd := m.login.Update(msg)
			if m.activeView == registerView {
				_, cmd = m.register.Update(msg)
			}
			return m, cmd
		}
		m.shuttingDown = true
		return m, tea.Sequence(m.shutdownCmd(), tea.Quit)
	}

	if key == "q" {
		if m.activeView == mainMenuView {
			m.shuttingDown = true
			return m, tea.Sequence(m.shutdownCmd(), tea.Quit)
		}
		m.activeView = mainMenuView
		m.errMsg = ""
		return m, nil
	}

	if key == "esc" && m.activeView != mainMenuView &&
		m.activeView != loginView && m.activeView != registerView {
		m.activeView = mainMenuView
		m.errMsg = ""
		return m, nil
	}

	if m.activeView == mainMenuView {
		switch key {
		case "1":
			m.activeView = chatView
		case "2":
			m.activeView = contactsView
		case "3":
			m.activeView = settingsView
		case "4":
			m.activeView = settingsView
		case "5":
			m.shuttingDown = true
			return m, tea.Sequence(m.shutdownCmd(), tea.Quit)
		}
		return m, nil
	}

	return m, m.dispatchToActiveView(msg)
}

func (m *Model) dispatchToActiveView(msg tea.Msg) tea.Cmd {
	switch m.activeView {
	case loginView:
		model, cmd := m.login.Update(msg)
		m.login = model.(LoginModel)
		return cmd

	case registerView:
		model, cmd := m.register.Update(msg)
		m.register = model.(RegisterModel)
		return cmd

	case chatView:
		if m.chat != nil {
			_, cmd := m.chat.Update(msg)
			return cmd
		}

	case contactsView:
		if m.contactsT != nil {
			_, cmd := m.contactsT.Update(msg)
			return cmd
		}

	case settingsView:
		sm, cmd := m.settings.Update(msg)
		m.settings = sm
		return cmd
	}
	return nil
}

func (m *Model) checkTransitions() {
	switch m.activeView {
	case loginView:
		if m.login.Done() && m.login.Result() != nil {
			m.currentUser = m.login.Result()
			m.activateMainMenu()
		} else if m.login.Done() {
			m.activeView = loginView
		}

	case registerView:
		if m.register.Done() && m.register.Result() != nil {
			m.currentUser = m.register.Result()
			m.activateMainMenu()
		} else if m.register.Done() {
			m.activeView = loginView
		}

	case chatView:
		if m.chat != nil && m.chat.Quitting() {
			m.activeView = mainMenuView
		}
	}
}

func (m *Model) activateMainMenu() {
	if m.currentUser == nil {
		return
	}
	u := m.currentUser

	m.chat = NewChatModel(m.db, m.cryptoSvc, u.Username, u.RawPassword)
	m.chat.SetSize(m.width, m.height-2)

	m.contactsT.SetCurrentUser(u.Username)

	pc := &passwordChanger{db: m.db, crypto: m.cryptoSvc}
	m.settings = NewSettingsModel(u, pc)
	m.settings.SetSize(m.width, m.height-2)
	m.settings.SetTorStatus(m.torOnline, 0, "")

	if contactList, err := storage.GetAllContacts(m.db, u.Username); err == nil {
		items := make([]contactItem, len(contactList))
		for i, c := range contactList {
			items[i] = contactItem{
				username:  c.ContactUsername,
				onionAddr: c.OnionAddress,
			}
		}
		m.contactsT.SetContacts(items)
	}

	m.activeView = mainMenuView
	m.errMsg = ""
}

func (m *Model) handleResize(w, h int) {
	m.width = w
	m.height = h
	m.login.SetSize(w, h)
	m.register.SetSize(w, h)
	m.settings.SetSize(w, h-2)
	if m.contactsT != nil {
		m.contactsT.SetSize(w, h-2)
	}
	if m.chat != nil {
		m.chat.SetSize(w, h-2)
	}
}

func (m Model) View() tea.View {
	if m.activeView == loginView {
		return m.login.View()
	}
	if m.activeView == registerView {
		return m.register.View()
	}

	header := m.renderHeader()
	statusBar := m.renderStatusBar()
	content := m.renderContent()
	composed := lip.JoinVertical(lip.Top, header, content, statusBar)
	return tea.NewView(composed)
}

func (m Model) renderHeader() string {
	return HeaderStyle.Width(m.width).Render("SOTE — Decentralized Messenger")
}

func (m Model) renderStatusBar() string {
	connDot := StatusDot(m.torOnline, false)
	torLabel := "disconnected"
	if m.torOnline {
		torLabel = "connected"
	}
	userLabel := "Not logged in"
	if m.currentUser != nil {
		userLabel = m.currentUser.Username
	}
	onionSection := ""
	if m.onionAddr != "" {
		onionSection = " | Onion: " + m.onionAddr
	}
	left := fmt.Sprintf("%s %s | %s%s | [%s]",
		connDot, torLabel, userLabel, onionSection, m.activeView.String())
	right := ""
	if m.errMsg != "" {
		right = ErrorStyle.Render(m.errMsg)
	}
	bar := StatusBarStyle.Width(m.width).Render(left)
	if right != "" {
		bar = lip.JoinHorizontal(lip.Top, bar, right)
	}
	return bar
}

func (m Model) renderContent() string {
	contentHeight := m.height - 2
	if contentHeight < 1 {
		contentHeight = 1
	}

	switch m.activeView {
	case mainMenuView:
		return m.renderMainMenu(m.width, contentHeight)
	case chatView:
		if m.chat != nil {
			return m.chat.View().Content
		}
		return RenderEmptyState(m.width, "Chat — select a contact")
	case contactsView:
		if m.contactsT != nil {
			return m.contactsT.View().Content
		}
		return RenderEmptyState(m.width, "Contacts — loading")
	case settingsView:
		return BaseStyle.Width(m.width).Height(contentHeight).Render(m.settings.View())
	default:
		return RenderEmptyState(m.width, "Unknown view")
	}
}

func (m Model) renderMainMenu(width, height int) string {
	menuItems := []struct {
		shortcut string
		label    string
	}{
		{"1", "Send Message"},
		{"2", "Contacts"},
		{"3", "Settings"},
		{"4", "Show Onion Address"},
		{"5", "Exit"},
	}

	s := MenuTitleStyle.Width(width).Render("Main Menu") + "\n\n"
	for _, item := range menuItems {
		s += MenuItemStyle.Width(width - 2).Render("[" + item.shortcut + "] " + item.label) + "\n"
	}
	s += "\n" + HelpStyle.Width(width).Render("1-5 navigate · q quit · esc go back")
	if m.errMsg != "" {
		s += "\n" + ErrorStyle.Width(width).Render(m.errMsg)
	}
	return BaseStyle.Width(width).Height(height).Render(s)
}

func (m *Model) shutdownCmd() tea.Cmd {
	return func() tea.Msg {
		slog.Info("shutting down SOTE...")
		// 1. Stop HTTP server, drain connections (30s timeout)
		if m.server != nil {
			if err := m.server.Stop(); err != nil {
				slog.Error("server stop", "error", err)
			}
		}
		// 2. Close Tor hidden service
		if m.torSvc != nil {
			if err := m.torSvc.Stop(); err != nil {
				slog.Error("tor stop", "error", err)
			}
		}
		// 3. Checkpoint SQLite WAL (flush to main DB file)
		if m.db != nil {
			if _, err := m.db.Exec("PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
				slog.Error("wal checkpoint", "error", err)
			}
			// 4. Close database
			m.db.Close()
		}
		// 5. Terminal is restored automatically by Bubble Tea on Quit
		slog.Info("SOTE stopped gracefully")
		return tea.Quit()
	}
}

func (m Model) OnionAddress() string { return m.onionAddr }
