package tui

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
	"sote/internal/domain"
)

type LoginResult struct {
	User *domain.User
	Err  error
}

type LoginModel struct {
	username textinput.Model
	password textinput.Model
	inputs   []textinput.Model
	err      string
	loading  bool
	focused  int
	done     bool
	result   *domain.User
	onSubmit func(username, password string) (*domain.User, error)
	width    int
	height   int
}

func NewLoginModel(onSubmit func(username, password string) (*domain.User, error)) LoginModel {
	u := textinput.New()
	u.Placeholder = "Enter username"
	u.CharLimit = 32
	u.Focus()

	p := textinput.New()
	p.Placeholder = "Enter password"
	p.EchoMode = textinput.EchoPassword
	p.EchoCharacter = '•'
	p.CharLimit = 128

	return LoginModel{
		username: u, password: p,
		inputs:  []textinput.Model{u, p},
		focused: 0, onSubmit: onSubmit,
	}
}

func (m LoginModel) Init() tea.Cmd { return nil }

func (m LoginModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case loginResultMsg:
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
		case "ctrl+c", "q":
			return m, tea.Quit
		case "esc":
			m.result = nil
			m.done = true
			return m, tea.Quit
		case "tab":
			m.err = ""
			m.focused = (m.focused + 1) % 2
			m.updateFocus()
		case "shift+tab":
			m.err = ""
			m.focused = (m.focused + 1) % 2
			m.updateFocus()
		case "enter":
			m.err = ""
			if m.focused == 0 || m.focused == 1 {
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

func (m LoginModel) View() tea.View {
	if m.done {
		if m.result != nil {
			return tea.NewView(lipgloss.Place(m.width, m.height,
				lipgloss.Center, lipgloss.Center,
				SuccessStyle.Render("Logged in as "+m.result.Username+"\n\nPress any key to continue")))
		}
		return tea.NewView(lipgloss.Place(m.width, m.height,
			lipgloss.Center, lipgloss.Center,
			MutedStyle.Render("Login cancelled")))
	}

	var b strings.Builder
	b.WriteString(PrimaryStyle.Bold(true).Render("SOTE") + MutedStyle.Render(" — Secure Messaging"))
	b.WriteString("\n\n")
	b.WriteString(PrimaryStyle.Bold(true).Render("Sign In"))
	b.WriteString("\n\n")

	labels := []string{"Username", "Password"}
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

	submitBtn := ButtonGhostStyle.Render("[ Submit ]")
	if m.focused == 0 || m.focused == 1 {
		submitBtn = ButtonStyle.Render("[ Submit ]")
	}
	b.WriteString(submitBtn)
	b.WriteString("\n\n")

	if m.err != "" {
		b.WriteString(ErrorStyle.Render(m.err))
		b.WriteString("\n\n")
	}
	b.WriteString(HelpStyle.Render("Tab/Shift+Tab: switch field  •  Enter: submit  •  Esc: back/cancel"))

	return tea.NewView(lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, b.String()))
}

func (m *LoginModel) updateFocus() {
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

func (m *LoginModel) updateInputs(msg tea.Msg) tea.Cmd {
	var cmds []tea.Cmd
	for i := range m.inputs {
		var cmd tea.Cmd
		m.inputs[i], cmd = m.inputs[i].Update(msg)
		cmds = append(cmds, cmd)
	}
	return tea.Batch(cmds...)
}

func (m *LoginModel) submit() tea.Cmd {
	username := strings.TrimSpace(m.inputs[0].Value())
	password := m.inputs[1].Value()
	if username == "" || password == "" {
		m.err = "Username and password are required"
		m.loading = false
		return nil
	}
	return func() tea.Msg {
		user, err := m.onSubmit(username, password)
		if err != nil {
			return loginResultMsg{err: err}
		}
		return loginResultMsg{user: user}
	}
}

type loginResultMsg struct {
	user *domain.User
	err  error
}

func (m LoginModel) Result() *domain.User { return m.result }
func (m LoginModel) Done() bool           { return m.done }
func (m *LoginModel) SetSize(width, height int) {
	m.width = width
	m.height = height
}
