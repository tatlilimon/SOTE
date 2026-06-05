package tui

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	"charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"sote/internal/domain"
	"sote/internal/messages"
	"sote/internal/storage"
)

type msgsLoadedMsg struct {
	messages []messages.DecryptedMessage
	err      error
}

type msgSentMsg struct {
	err error
}

type ChatModel struct {
	viewport *viewport.Model
	textarea *textarea.Model

	username string
	password string

	contacts    []storage.Contact
	contactIdx  int
	contactName string

	displayMsgs []messages.DecryptedMessage

	db     *sql.DB
	crypto domain.CryptoService

	width  int
	height int

	ready    bool
	loading  bool
	errMsg   string
	focused  bool
	quitting bool
}

func NewChatModel(db *sql.DB, crypto domain.CryptoService, username, password string) *ChatModel {
	ta := textarea.New()
	ta.Placeholder = "Type a message..."
	ta.ShowLineNumbers = false
	ta.CharLimit = int(messages.MaxMessageSize)
	ta.SetHeight(3)

	vp := viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))

	contacts, _ := storage.GetAllContacts(db, username)

	cm := &ChatModel{
		viewport: &vp,
		textarea: &ta,
		username: username,
		password: password,
		db:       db,
		crypto:   crypto,
		contacts: contacts,
		ready:    true,
	}
	if len(contacts) > 0 {
		cm.contactIdx = 0
		cm.contactName = contacts[0].ContactUsername
	}
	return cm
}

func (m *ChatModel) Init() tea.Cmd {
	return tea.Batch(
		textarea.Blink,
		m.loadMessagesCmd(),
	)
}

func (m *ChatModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.resizeComponents()

	case msgsLoadedMsg:
		m.loading = false
		if msg.err != nil {
			m.errMsg = fmt.Sprintf("Failed to load messages: %v", msg.err)
		} else {
			m.displayMsgs = msg.messages
			m.errMsg = ""
			m.renderMessages()
			m.viewport.GotoBottom()
		}

	case msgSentMsg:
		if msg.err != nil {
			m.errMsg = fmt.Sprintf("Send failed: %v", msg.err)
		} else {
			m.loading = true
			cmds = append(cmds, m.loadMessagesCmd())
		}

	case tea.KeyMsg:
		if !m.focused {
			break
		}
		switch msg.String() {
		case "left":
			if len(m.contacts) > 0 {
				m.contactIdx = (m.contactIdx - 1 + len(m.contacts)) % len(m.contacts)
				m.contactName = m.contacts[m.contactIdx].ContactUsername
				m.displayMsgs = nil
				m.errMsg = ""
				m.loading = true
				cmds = append(cmds, m.loadMessagesCmd())
			}
		case "right":
			if len(m.contacts) > 0 {
				m.contactIdx = (m.contactIdx + 1) % len(m.contacts)
				m.contactName = m.contacts[m.contactIdx].ContactUsername
				m.displayMsgs = nil
				m.errMsg = ""
				m.loading = true
				cmds = append(cmds, m.loadMessagesCmd())
			}
		case "enter":
			v := strings.TrimSpace(m.textarea.Value())
			if v != "" && m.contactName != "" {
				m.textarea.Reset()
				cmds = append(cmds, m.sendMessageCmd(v))
			}
		case "esc":
			m.quitting = true
			return m, nil
		default:
			var cmd tea.Cmd
			*m.textarea, cmd = m.textarea.Update(msg)
			cmds = append(cmds, cmd)
			return m, tea.Batch(cmds...)
		}

	default:
		var vpCmd, taCmd tea.Cmd
		*m.viewport, vpCmd = m.viewport.Update(msg)
		*m.textarea, taCmd = m.textarea.Update(msg)
		cmds = append(cmds, vpCmd, taCmd)
	}

	return m, tea.Batch(cmds...)
}

func (m *ChatModel) View() tea.View {
	if !m.ready {
		return tea.NewView("Initializing chat...")
	}
	content := lipgloss.JoinVertical(lipgloss.Top,
		m.renderHeader(),
		m.viewport.View(),
		m.textarea.View(),
		m.renderFooter(),
	)
	return tea.NewView(content)
}

func (m *ChatModel) Quitting() bool                        { return m.quitting }
func (m *ChatModel) Messages() []messages.DecryptedMessage { return m.displayMsgs }
func (m *ChatModel) ContactName() string                   { return m.contactName }
func (m *ChatModel) TextareaValue() string                 { return m.textarea.Value() }
func (m *ChatModel) Loading() bool                         { return m.loading }
func (m *ChatModel) Error() string                         { return m.errMsg }

func (m *ChatModel) Focus() tea.Cmd {
	m.focused = true
	return m.textarea.Focus()
}

func (m *ChatModel) Blur() {
	m.focused = false
	m.textarea.Blur()
}

func (m *ChatModel) SetContacts(contacts []storage.Contact) {
	m.contacts = contacts
	if m.contactName == "" && len(contacts) > 0 {
		m.contactIdx = 0
		m.contactName = contacts[0].ContactUsername
	}
}

func (m *ChatModel) SetSize(w, h int) {
	m.width = w
	m.height = h
	m.resizeComponents()
}

func (m *ChatModel) resizeComponents() {
	if m.width == 0 || m.height == 0 {
		return
	}
	headerHeight := lipgloss.Height(m.renderHeader())
	footerHeight := lipgloss.Height(m.renderFooter())
	taHeight := 3
	vpHeight := m.height - headerHeight - taHeight - footerHeight
	if vpHeight < 1 {
		vpHeight = 1
	}
	m.viewport.SetWidth(m.width)
	m.viewport.SetHeight(vpHeight)
	m.textarea.SetWidth(m.width)
	m.textarea.SetHeight(taHeight)
}

func (m *ChatModel) renderHeader() string {
	if len(m.contacts) == 0 {
		return ContactHeaderStyle.Width(m.width).Render("Chat — No contacts")
	}
	left, right := "\u2190", "\u2192"
	if len(m.contacts) > 1 {
		left, right = "\u25C4", "\u25BA"
	}
	return ContactHeaderStyle.Width(m.width).Render(
		fmt.Sprintf(" %s Chat with: %s %s ", left, m.contactName, right),
	)
}

func (m *ChatModel) renderFooter() string {
	return HelpStyle.Width(m.width).Render(
		"Enter: send  |  Shift+Enter: newline  |  \u2190 \u2192 : switch contact  |  Esc: back",
	)
}

func (m *ChatModel) renderMessages() {
	if len(m.displayMsgs) == 0 {
		m.viewport.SetContent(
			RenderEmptyState(m.width, "No messages yet. Type a message and press Enter to send."),
		)
		return
	}
	var sb strings.Builder
	for _, msg := range m.displayMsgs {
		sb.WriteString(m.formatMessage(msg))
		sb.WriteString("\n")
	}
	m.viewport.SetContent(sb.String())
}

func (m *ChatModel) formatMessage(msg messages.DecryptedMessage) string {
	ts := formatTimestamp(msg.Timestamp)
	isSent := msg.Sender == m.username
	var line string
	if isSent {
		line = fmt.Sprintf("%s %s", msg.Content, ts)
	} else {
		line = fmt.Sprintf("%s %s: %s", ts, msg.Sender, msg.Content)
	}
	if isSent {
		return lipgloss.PlaceHorizontal(m.width, lipgloss.Right, SentMsgStyle.Render(line))
	}
	return lipgloss.PlaceHorizontal(m.width, lipgloss.Left, ReceivedMsgStyle.Render(line))
}

func formatTimestamp(ts string) string {
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		for _, layout := range []string{"2006-01-02 15:04:05", "2006-01-02T15:04:05Z"} {
			if t, err = time.Parse(layout, ts); err == nil {
				break
			}
		}
		if err != nil {
			return ""
		}
	}
	return TimestampStyle.Render("[" + t.Format("15:04") + "]")
}

func (m *ChatModel) loadMessagesCmd() tea.Cmd {
	if m.contactName == "" {
		return nil
	}
	username, contact, password := m.username, m.contactName, m.password
	db, crypto := m.db, m.crypto

	return func() tea.Msg {
		svc := messages.NewService(db, crypto)
		msgs, err := svc.FetchMessages(context.Background(), username, contact, password)
		return msgsLoadedMsg{messages: msgs, err: err}
	}
}

func (m *ChatModel) sendMessageCmd(text string) tea.Cmd {
	sender, receiver, password := m.username, m.contactName, m.password
	db, crypto := m.db, m.crypto

	return func() tea.Msg {
		svc := messages.NewService(db, crypto)
		err := svc.SendMessage(context.Background(), sender, receiver, password, text)
		return msgSentMsg{err: err}
	}
}
