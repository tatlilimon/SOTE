package tui

import (
	"strings"
	"unicode"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
	"sote/internal/domain"
)

type RegisterModel struct {
	username        textinput.Model
	password        textinput.Model
	confirmPassword textinput.Model
	inputs          []textinput.Model
	err             string
	loading         bool
	focused         int
	done            bool
	result          *domain.User
	onSubmit        func(username, password string) (*domain.User, error)
	width           int
	height          int
}

func NewRegisterModel(onSubmit func(username, password string) (*domain.User, error)) RegisterModel {
	u := textinput.New()
	u.Placeholder = "Choose a username"
	u.CharLimit = 32
	u.Focus()

	p := textinput.New()
	p.Placeholder = "Choose a password (min 8 chars)"
	p.EchoMode = textinput.EchoPassword
	p.EchoCharacter = '•'
	p.CharLimit = 128

	c := textinput.New()
	c.Placeholder = "Confirm your password"
	c.EchoMode = textinput.EchoPassword
	c.EchoCharacter = '•'
	c.CharLimit = 128

	return RegisterModel{
		username: u, password: p, confirmPassword: c,
		inputs:  []textinput.Model{u, p, c},
		focused: 0, onSubmit: onSubmit,
	}
}

func (m RegisterModel) Init() tea.Cmd { return nil }

func (m RegisterModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case registerResultMsg:
		m.loading = false
		if msg.err != nil {
			m.err = msg.err.Error()
			return m, nil
		}
		m.result = msg.user
		m.done = true
		return m, nil

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tea.KeyPressMsg:
		if m.loading {
			switch msg.String() {
			case "ctrl+c", "q":
				return m, tea.Quit
			}
			return m, nil
		}
		if m.done {
			switch msg.String() {
			case "ctrl+c", "q", "esc", "enter":
				return m, tea.Quit
			}
			return m, nil
		}
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "esc":
			m.result = nil
			m.done = true
			return m, tea.Quit
		case "tab":
			m.err = ""
			m.focused = (m.focused + 1) % 3
			m.updateFocus()
		case "shift+tab":
			m.err = ""
			m.focused--
			if m.focused < 0 {
				m.focused = 2
			}
			m.updateFocus()
		case "enter":
			m.err = ""
			if m.focused >= 0 && m.focused <= 2 {
				m.loading = true
				return m, m.submit()
			}
		}
	}

	if !m.done {
		return m, m.updateInputs(msg)
	}
	return m, nil
}

func (m RegisterModel) View() tea.View {
	if m.done {
		if m.result != nil {
			return tea.NewView(lipgloss.Place(m.width, m.height,
				lipgloss.Center, lipgloss.Center,
				SuccessStyle.Render("Account created: "+m.result.Username+"\n\nYou are now logged in.\n\nPress any key to continue")))
		}
		return tea.NewView(lipgloss.Place(m.width, m.height,
			lipgloss.Center, lipgloss.Center,
			MutedStyle.Render("Registration cancelled")))
	}

	var b strings.Builder
	b.WriteString(PrimaryStyle.Bold(true).Render("SOTE") + MutedStyle.Render(" — Secure Messaging"))
	b.WriteString("\n\n")
	b.WriteString(PrimaryStyle.Bold(true).Render("Create Account"))
	b.WriteString("\n\n")

	labels := []string{"Username", "Password", "Confirm"}
	for i := range m.inputs {
		prompt := PromptStyle
		if m.focused == i {
			prompt = PromptFocusedStyle
		}
		b.WriteString(prompt.Render(labels[i]))
		b.WriteString("\n")
		b.WriteString(m.inputs[i].View())
		b.WriteString("\n\n")
	}

	if m.err != "" {
		b.WriteString(ErrorStyle.Render(m.err))
		b.WriteString("\n\n")
	}
	submitBtn := ButtonGhostStyle.Render("[ Create Account ]")
	if m.focused >= 0 && m.focused <= 2 {
		submitBtn = ButtonStyle.Render("[ Create Account ]")
	}
	b.WriteString(submitBtn)
	b.WriteString("\n\n")
	b.WriteString(HelpStyle.Render("Tab/Shift+Tab: next/prev field  •  Enter: submit  •  Esc: back/cancel"))

	return tea.NewView(lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, b.String()))
}

func (m *RegisterModel) updateFocus() {
	for i := range m.inputs {
		if i == m.focused {
			m.inputs[i].Focus()
			m.inputs[i].Prompt = ""
		} else {
			m.inputs[i].Blur()
			m.inputs[i].Prompt = ""
		}
	}
}

func (m *RegisterModel) updateInputs(msg tea.Msg) tea.Cmd {
	var cmds []tea.Cmd
	for i := range m.inputs {
		var cmd tea.Cmd
		m.inputs[i], cmd = m.inputs[i].Update(msg)
		cmds = append(cmds, cmd)
	}
	return tea.Batch(cmds...)
}

func (m *RegisterModel) submit() tea.Cmd {
	username := strings.TrimSpace(m.inputs[0].Value())
	password := m.inputs[1].Value()
	confirm := m.inputs[2].Value()

	if username == "" {
		m.err = "Username is required"
		m.loading = false
		return nil
	}
	if !isValidUsername(username) {
		m.err = "Username must be 3-32 alphanumeric characters or underscores"
		m.loading = false
		return nil
	}
	if password == "" {
		m.err = "Password is required"
		m.loading = false
		return nil
	}
	if len(password) < 8 {
		m.err = "Password must be at least 8 characters"
		m.loading = false
		return nil
	}
	if password != confirm {
		m.err = "Passwords do not match"
		m.loading = false
		return nil
	}

	return func() tea.Msg {
		user, err := m.onSubmit(username, password)
		if err != nil {
			return registerResultMsg{err: err}
		}
		return registerResultMsg{user: user}
	}
}

type registerResultMsg struct {
	user *domain.User
	err  error
}

func isValidUsername(username string) bool {
	if len(username) < 3 || len(username) > 32 {
		return false
	}
	for _, r := range username {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' {
			return false
		}
	}
	return true
}

func (m RegisterModel) Result() *domain.User { return m.result }
func (m RegisterModel) Done() bool           { return m.done }
func (m *RegisterModel) SetSize(width, height int) {
	m.width = width
	m.height = height
}
