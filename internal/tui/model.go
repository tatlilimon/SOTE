package tui

import (
	"context"
	"database/sql"
	"errors"
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
	"sote/internal/integration"
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

// NodeEventMsg carries a node event (Tor state, incoming message, contact
// request, delivery result) into the Bubble Tea loop.
type NodeEventMsg struct{ Event integration.Event }

type passwordChanger struct {
	db     *sql.DB
	crypto *crypto.Service
}

// ChangePassword delegates to the auth service, which updates the password
// hash, the encrypted private key and stored message copies in one
// transaction. (It used to INSERT a second user row, which always failed.)
func (pc *passwordChanger) ChangePassword(username, currentPassword, newPassword string) error {
	return auth.New(pc.db).ChangePassword(context.Background(), username, currentPassword, newPassword)
}

type Model struct {
	cfg        *config.Config
	db         *sql.DB
	authSvc    *auth.AuthService
	cryptoSvc  *crypto.Service
	contactSvc *contacts.Service
	msgSvc     *messages.Service
	torSvc     *tor.TorService
	server     *transport.Server
	client     *transport.Client

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
	notice       string
	quitting     bool
	shuttingDown bool

	// deps connects the model to the running node; nil in unit tests.
	deps       *integration.Deps
	justLogged bool
}

// WithDeps connects the model to a node: Tor starts from Init, login unlocks
// the identity, and contacts, requests and messages go over the network.
func (m Model) WithDeps(d integration.Deps) Model {
	m.deps = &d
	m.onionAddr = d.Node.OnionAddress()
	m.contactsT.SetActions(m.contactActions())
	return m
}

func itemFromContact(c *storage.Contact) contactItem {
	if c == nil {
		return contactItem{}
	}
	return contactItem{username: c.ContactUsername, onionAddr: c.OnionAddress, id: c.ID}
}

func (m *Model) contactActions() ContactActions {
	node := m.deps.Node
	return ContactActions{
		Add: func(onion string) (contactItem, error) {
			c, err := node.AddContact(context.Background(), onion)
			if errors.Is(err, contacts.ErrAlreadyContact) {
				return contactItem{}, fmt.Errorf("%s is already a contact", c.ContactUsername)
			}
			if err != nil {
				return contactItem{}, err
			}
			return itemFromContact(c), nil
		},
		Accept: func(id int64) (contactItem, error) {
			c, err := node.AcceptRequest(context.Background(), id)
			if err != nil {
				return contactItem{}, err
			}
			return itemFromContact(c), nil
		},
		Decline: func(id int64) error { return node.DeclineRequest(context.Background(), id) },
	}
}

func (m *Model) loadPendingCmd() tea.Cmd {
	if m.deps == nil {
		return nil
	}
	node := m.deps.Node
	return func() tea.Msg {
		reqs, err := node.PendingRequests(context.Background())
		if err != nil {
			return nil
		}
		items := make([]contactItem, len(reqs))
		for i, r := range reqs {
			items[i] = contactItem{username: r.FromUsername, onionAddr: r.FromOnion, pending: true, requestID: r.ID}
		}
		return ContactsPendingMsg{Requests: items}
	}
}

func (m *Model) reloadContacts() {
	if m.currentUser == nil {
		return
	}
	list, err := storage.GetAllContacts(m.db, m.currentUser.Username)
	if err != nil {
		return
	}
	items := make([]contactItem, len(list))
	for i, c := range list {
		items[i] = contactItem{username: c.ContactUsername, onionAddr: c.OnionAddress, id: c.ID}
	}
	m.contactsT.SetContacts(items)
	if m.chat != nil {
		m.chat.SetContacts(list)
	}
}

func (m Model) handleNodeEvent(e integration.Event) (tea.Model, tea.Cmd) {
	switch e.Kind {
	case integration.EventTorStarting:
		m.torOnline = false
		m.notice = "Connecting to Tor…"
	case integration.EventTorReady:
		m.torOnline = true
		m.notice = ""
		if m.deps != nil && m.deps.Tor != nil {
			m.settings.SetTorStatus(true, m.deps.Tor.SocksPort(), "")
		}
	case integration.EventTorFailed:
		m.torOnline = false
		m.notice = ""
		m.errMsg = "Tor: " + e.Detail
		m.settings.SetTorStatus(false, 0, "")
	case integration.EventMessageReceived, integration.EventDelivery:
		if m.chat != nil && m.chat.ContactName() == e.Peer {
			return m, m.chat.LoadMessagesCmd()
		}
		if e.Kind == integration.EventMessageReceived {
			m.notice = "New message from " + e.Peer
		}
	case integration.EventContactRequest:
		m.notice = "Contact request from " + e.Peer + " — open Contacts and press r"
		return m, m.loadPendingCmd()
	}
	return m, nil
}

// viewCapturesKeys reports whether the active view needs every key (typing a
// message, an address or a password), so global shortcuts like q and Esc
// must not steal them.
func (m Model) viewCapturesKeys() bool {
	switch m.activeView {
	case chatView:
		return m.chat != nil
	case contactsView:
		return m.contactsT != nil && m.contactsT.state != contactStateViewing
	case settingsView:
		return m.settings.ShowsForm()
	}
	return false
}

func (m *Model) enterChat() tea.Cmd {
	if m.chat == nil {
		return nil
	}
	m.reloadContacts()
	m.activeView = chatView
	return tea.Batch(m.chat.Focus(), m.chat.LoadMessagesCmd())
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
	if m.deps != nil {
		node := m.deps.Node
		return func() tea.Msg {
			node.Start(context.Background())
			return nil
		}
	}
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

	case NodeEventMsg:
		return m.handleNodeEvent(msg.Event)

	case ContactsPendingMsg:
		m.contactsT.SetPendingRequests(msg.Requests)
		return m, nil

	case BackMsg:
		m.activeView = mainMenuView
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
	if pc, ok := msg.(PasswordChangedMsg); ok && pc.Error == nil && m.chat != nil && m.currentUser != nil {
		m.chat.SetPassword(m.currentUser.RawPassword)
	}
	m.checkTransitions()
	if m.justLogged {
		m.justLogged = false
		cmds = append(cmds, m.loadPendingCmd())
	}
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

	if m.viewCapturesKeys() {
		cmd := m.dispatchToActiveView(msg)
		m.checkTransitions()
		return m, cmd
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
		m.notice = ""
		switch key {
		case "1":
			return m, m.enterChat()
		case "2":
			m.reloadContacts()
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

	if m.deps != nil {
		if err := m.deps.Node.Login(u); err != nil {
			m.errMsg = "Could not unlock your key: " + err.Error()
		}
		m.justLogged = true
	}

	m.chat = NewChatModel(m.db, m.cryptoSvc, u.Username, u.RawPassword)
	m.chat.SetSize(m.width, m.height-2)
	if m.deps != nil {
		m.chat.SetService(m.deps.Messages)
	}

	m.contactsT.SetCurrentUser(u.Username)

	pc := &passwordChanger{db: m.db, crypto: m.cryptoSvc}
	m.settings = NewSettingsModel(u, pc)
	m.settings.SetSize(m.width, m.height-2)
	socks := 0
	if m.torOnline && m.deps != nil && m.deps.Tor != nil {
		socks = m.deps.Tor.SocksPort()
	}
	m.settings.SetTorStatus(m.torOnline, socks, "")
	m.settings.SetOnion(m.onionAddr)
	if m.deps != nil {
		d := *m.deps
		m.settings.SetBridges(d.Config.UseBridges, config.BridgeText(d.Config.Bridges, d.Config.ClientTransportPlugins),
			len(d.Config.Bridges), d.SaveBridges)
	}

	m.reloadContacts()

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
	} else if m.notice != "" {
		right = MutedStyle.Render(m.notice)
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
		s += MenuItemStyle.Width(width-2).Render("["+item.shortcut+"] "+item.label) + "\n"
	}
	s += "\n" + HelpStyle.Width(width).Render("1-5 navigate · q quit · esc go back")
	if m.errMsg != "" {
		s += "\n" + ErrorStyle.Width(width).Render(m.errMsg)
	}
	return BaseStyle.Width(width).Height(height).Render(s)
}

func (m *Model) shutdownCmd() tea.Cmd {
	if m.deps != nil {
		// main stops the node and closes the database after the program
		// exits, so nothing here can race the event loop.
		return func() tea.Msg { return tea.Quit() }
	}
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
