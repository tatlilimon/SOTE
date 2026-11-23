package tui

import (
	"fmt"
	"io"
	"strings"

	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"sote/internal/peer"
)

type contactState int

const (
	contactStateViewing contactState = iota
	contactStateAdding
	contactStatePending
)

type contactItem struct {
	username  string
	onionAddr string
	pending   bool
	// id is the contact's row ID; requestID the pending request's ID.
	id        int
	requestID int64
}

// ContactActions performs the network side of the contacts screen. Without
// them (unit tests) the screen only edits its in-memory list.
type ContactActions struct {
	// Add fetches the identity served at an onion address, stores the
	// contact and queues a contact request.
	Add func(onion string) (contactItem, error)
	// Accept adds the requester after verifying its onion identity.
	Accept func(requestID int64) (contactItem, error)
	// Decline declines a request.
	Decline func(requestID int64) error
}

func (c contactItem) Title() string {
	if c.pending {
		return c.username + " (pending request)"
	}
	return c.username
}

func (c contactItem) Description() string { return c.onionAddr }
func (c contactItem) FilterValue() string { return c.username + " " + c.onionAddr }

type ContactsLoadedMsg struct {
	Contacts []contactItem
}

type ContactsPendingMsg struct {
	Requests []contactItem
}

type ContactAddedMsg struct {
	Item contactItem
	Err  error
}

type ContactRequestHandledMsg struct {
	Username string
	Accepted bool
	Item     contactItem
	Err      error
}

type contactDelegate struct {
	list.DefaultDelegate
}

func (d contactDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	c, ok := item.(contactItem)
	if !ok {
		d.DefaultDelegate.Render(w, m, index, item)
		return
	}

	var (
		isSelected = index == m.Index()
		dot        string
	)

	if c.pending {
		dot = WarningStyle.Render("\u25CF")
	} else {
		dot = SuccessStyle.Render("\u25CF")
	}

	title := c.Title()
	desc := c.Description()

	if isSelected {
		title = lipgloss.NewStyle().Foreground(ColorPrimary).Bold(true).Render("> " + title)
		desc = lipgloss.NewStyle().Foreground(ColorSecondary).Render(desc)
	} else {
		title = TextStyle.Render(title)
		desc = MutedStyle.Render(desc)
	}

	fmt.Fprintf(w, " %s %s\n", dot, title)
	fmt.Fprintf(w, "   %s\n", desc)
}

func (d contactDelegate) Height() int  { return 3 }
func (d contactDelegate) Spacing() int { return 0 }

type ContactsModel struct {
	list        list.Model
	input       textinput.Model
	state       contactState
	width       int
	height      int
	err         string
	currentUser string

	contacts    []contactItem
	pendingReqs []contactItem

	actions ContactActions
	busy    string
}

// SetActions connects the screen to the node.
func (m *ContactsModel) SetActions(a ContactActions) { m.actions = a }

// Busy returns the progress note shown while a network action runs.
func (m *ContactsModel) Busy() string { return m.busy }

func NewContactsModel(currentUser string) ContactsModel {
	ti := textinput.New()
	ti.Placeholder = "Paste the contact's .onion address"
	// A v3 address is 62 characters; leave room for a pasted https:// prefix.
	ti.CharLimit = 100
	ti.SetWidth(50)
	ti.Focus()

	delegate := contactDelegate{DefaultDelegate: list.NewDefaultDelegate()}

	l := list.New([]list.Item{}, delegate, 80, 20)
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetShowFilter(false)
	l.SetShowHelp(false)
	l.DisableQuitKeybindings()

	return ContactsModel{
		list:        l,
		input:       ti,
		state:       contactStateViewing,
		currentUser: currentUser,
	}
}

func (m *ContactsModel) SetCurrentUser(username string) {
	m.currentUser = username
}

func (m *ContactsModel) AppendPending(item contactItem) {
	m.pendingReqs = append(m.pendingReqs, item)
	if m.state == contactStatePending {
		m.rebuildList()
	}
}

func (m *ContactsModel) SetSize(width, height int) {
	m.width = width
	m.height = height
	m.list.SetSize(width, height-2)
	m.input.SetWidth(max(min(width-4, 50), 10))
}

func (m *ContactsModel) SetContacts(contacts []contactItem) {
	m.contacts = contacts
	m.rebuildList()
}

func (m *ContactsModel) SetPendingRequests(reqs []contactItem) {
	m.pendingReqs = reqs
	if m.state == contactStatePending {
		m.rebuildList()
	}
}

func (m *ContactsModel) rebuildList() {
	var items []list.Item
	var source []contactItem

	switch m.state {
	case contactStatePending:
		source = m.pendingReqs
	default:
		source = m.contacts
	}

	for _, c := range source {
		items = append(items, c)
	}
	m.list.SetItems(items)
}

func (m ContactsModel) Init() tea.Cmd {
	return nil
}

func (m ContactsModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.SetSize(msg.Width, msg.Height)

	case tea.KeyMsg:
		if m.state == contactStateAdding {
			if k := msg.String(); k != "enter" && k != "esc" {
				// Typing goes to the address field (it never reached it
				// before, so no address could be entered).
				var cmd tea.Cmd
				m.input, cmd = m.input.Update(msg)
				return m, cmd
			}
		}
		cmd := m.handleKey(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
		return m, tea.Batch(cmds...)

	case ContactsLoadedMsg:
		m.SetContacts(msg.Contacts)
		m.err = ""

	case ContactsPendingMsg:
		m.SetPendingRequests(msg.Requests)
		m.err = ""

	case ContactAddedMsg:
		m.busy = ""
		if msg.Err != nil {
			m.err = msg.Err.Error()
		} else {
			m.contacts = append(m.contacts, msg.Item)
			m.state = contactStateViewing
			m.rebuildList()
			m.err = ""
			m.input.Reset()
		}

	case ContactRequestHandledMsg:
		m.busy = ""
		if msg.Err != nil {
			m.err = msg.Err.Error()
			break
		}
		if msg.Accepted {
			m.err = fmt.Sprintf("Accepted request from %s", msg.Username)
			if msg.Item.username != "" {
				m.contacts = append(m.contacts, msg.Item)
			}
		} else {
			m.err = fmt.Sprintf("Declined request from %s", msg.Username)
		}
		m.removePending(msg.Username)

	default:
		switch m.state {
		case contactStateAdding:
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			cmds = append(cmds, cmd)
		default:
			var cmd tea.Cmd
			m.list, cmd = m.list.Update(msg)
			cmds = append(cmds, cmd)
		}
	}

	return m, tea.Batch(cmds...)
}

func (m *ContactsModel) removePending(username string) {
	filtered := make([]contactItem, 0, len(m.pendingReqs))
	for _, r := range m.pendingReqs {
		if r.username != username {
			filtered = append(filtered, r)
		}
	}
	m.SetPendingRequests(filtered)
}

func (m *ContactsModel) handleKey(msg tea.KeyMsg) tea.Cmd {
	key := msg.String()

	switch m.state {
	case contactStateAdding:
		return m.handleAddingKey(key)
	case contactStatePending:
		return m.handlePendingKey(key)
	default:
		return m.handleViewingKey(key)
	}
}

func (m *ContactsModel) handleViewingKey(key string) tea.Cmd {
	switch key {
	case "a", "A":
		m.state = contactStateAdding
		m.input.Focus()
		m.err = ""
		return nil

	case "r", "R":
		m.state = contactStatePending
		m.rebuildList()
		m.err = ""
		return nil

	case "enter":
		sel := m.list.SelectedItem()
		if sel != nil {
			c := sel.(contactItem)
			m.err = fmt.Sprintf("Contact: %s (%s)", c.username, truncateOnion(c.onionAddr))
		}
		return nil

	case "esc":
		if m.state == contactStatePending {
			m.state = contactStateViewing
			m.rebuildList()
			m.err = ""
			return nil
		}
		return func() tea.Msg { return BackMsg{} }

	case "q", "ctrl+c":
		return tea.Quit

	default:
		return nil
	}
}

func (m *ContactsModel) handleAddingKey(key string) tea.Cmd {
	switch key {
	case "enter":
		addr := strings.TrimSpace(m.input.Value())
		if addr == "" {
			m.err = "Onion address cannot be empty"
			return nil
		}
		if !strings.HasSuffix(strings.TrimSuffix(addr, "/"), ".onion") {
			m.err = "Address must end with .onion"
			return nil
		}
		if m.actions.Add != nil {
			onion, err := peer.NormalizeOnion(addr)
			if err != nil {
				m.err = "Not a valid v3 onion address (56 characters followed by .onion)"
				return nil
			}
			addr = onion
		}
		return m.submitAddContact(addr)

	case "esc":
		m.state = contactStateViewing
		m.rebuildList()
		m.err = ""
		m.input.Reset()
		return nil

	default:
		return nil
	}
}

func (m *ContactsModel) handlePendingKey(key string) tea.Cmd {
	switch key {
	case "a", "A":
		sel := m.list.SelectedItem()
		if sel != nil {
			c := sel.(contactItem)
			return m.handleAcceptRequest(c)
		}
		return nil

	case "d", "D":
		sel := m.list.SelectedItem()
		if sel != nil {
			c := sel.(contactItem)
			return m.handleDeclineRequest(c)
		}
		return nil

	case "esc", "r", "R":
		m.state = contactStateViewing
		m.rebuildList()
		m.err = ""
		return nil

	case "q", "ctrl+c":
		return tea.Quit

	default:
		return nil
	}
}

func (m *ContactsModel) submitAddContact(addr string) tea.Cmd {
	if add := m.actions.Add; add != nil {
		m.err = ""
		m.busy = "Contacting " + truncateOnion(addr) + " over Tor…"
		return func() tea.Msg {
			item, err := add(addr)
			return ContactAddedMsg{Item: item, Err: err}
		}
	}
	return func() tea.Msg {
		return ContactAddedMsg{
			Item: contactItem{
				username:  extractUsername(addr),
				onionAddr: addr,
				pending:   false,
			},
		}
	}
}

func (m *ContactsModel) handleAcceptRequest(c contactItem) tea.Cmd {
	if accept := m.actions.Accept; accept != nil {
		m.busy = "Checking " + c.username + "'s onion identity over Tor…"
		return func() tea.Msg {
			item, err := accept(c.requestID)
			return ContactRequestHandledMsg{Username: c.username, Accepted: true, Item: item, Err: err}
		}
	}
	return func() tea.Msg {
		return ContactRequestHandledMsg{Username: c.username, Accepted: true}
	}
}

func (m *ContactsModel) handleDeclineRequest(c contactItem) tea.Cmd {
	if decline := m.actions.Decline; decline != nil {
		return func() tea.Msg {
			return ContactRequestHandledMsg{Username: c.username, Accepted: false, Err: decline(c.requestID)}
		}
	}
	return func() tea.Msg {
		return ContactRequestHandledMsg{Username: c.username, Accepted: false}
	}
}

func (m ContactsModel) View() tea.View {
	if m.width == 0 || m.height == 0 {
		m.width = 80
		m.height = 24
	}

	var content string

	switch m.state {
	case contactStateAdding:
		content = m.viewAddForm()
	case contactStatePending:
		content = m.viewPendingList()
	default:
		content = m.viewContactList()
	}

	if m.err != "" && m.state != contactStateViewing {
		content = lipgloss.JoinVertical(lipgloss.Left, ErrorStyle.Render("  "+m.err), content)
	}
	if m.busy != "" {
		content = lipgloss.JoinVertical(lipgloss.Left, MutedStyle.Render("  "+m.busy), content)
	}

	help := m.viewHelp()

	str := lipgloss.JoinVertical(
		lipgloss.Left,
		m.viewHeader(),
		content,
		help,
	)
	return tea.NewView(str)
}

func (m ContactsModel) viewHeader() string {
	var title string
	switch m.state {
	case contactStateAdding:
		title = "Add Contact"
	case contactStatePending:
		title = "Pending Requests"
	default:
		title = "Contacts"
	}
	return HeaderStyle.Width(m.width).Render(title)
}

func (m ContactsModel) viewContactList() string {
	if len(m.contacts) == 0 {
		return RenderEmptyState(m.width, "No contacts yet. Press 'a' to add your first contact.")
	}
	if m.err != "" {
		return lipgloss.JoinVertical(
			lipgloss.Left,
			SuccessStyle.Render("  "+m.err),
			m.list.View(),
		)
	}
	return m.list.View()
}

func (m ContactsModel) viewAddForm() string {
	form := lipgloss.JoinVertical(
		lipgloss.Left,
		TextStyle.Render("  Enter the .onion address of the contact you want to add:"),
		"",
		lipgloss.NewStyle().PaddingLeft(2).Render(m.input.View()),
		"",
		HelpStyle.Render("  Press Enter to send request, Esc to cancel"),
	)
	return form
}

func (m ContactsModel) viewPendingList() string {
	if len(m.pendingReqs) == 0 {
		return RenderEmptyState(m.width, "No pending contact requests.")
	}
	return m.list.View()
}

func (m ContactsModel) viewHelp() string {
	switch m.state {
	case contactStateAdding:
		return HelpStyle.Render(" enter: submit  •  esc: cancel")
	case contactStatePending:
		return HelpStyle.Render(" a: accept  •  d: decline  •  r/esc: back")
	default:
		if len(m.contacts) == 0 {
			return HelpStyle.Render(" a: add contact  •  r: pending requests  •  q: quit")
		}
		return HelpStyle.Render(" a: add  •  r: requests  •  enter: details  •  esc: back  •  q: quit")
	}
}

func extractUsername(addr string) string {
	clean := strings.TrimSuffix(strings.TrimPrefix(addr, "http://"), "https://")
	clean = strings.TrimSuffix(clean, ".onion")
	if idx := strings.LastIndex(clean, "/"); idx >= 0 {
		clean = clean[idx+1:]
	}
	if clean == "" {
		return "unknown"
	}
	return clean
}

func truncateOnion(addr string) string {
	if len(addr) > 24 {
		return addr[:20] + "..."
	}
	return addr
}

type BackMsg struct{}
