//go:build headless

package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type screen int

const (
	screenServers screen = iota
	screenEditor
	screenMods
)

// rootModel owns config state and dispatches to the active screen.
type rootModel struct {
	cfg      *ServersConfig
	cfgErr   error
	screen   screen
	servers  *serversModel
	editor   *editorModel
	mods     *modsModel
	width    int
	height   int
	lastMsg  string
	quitting bool
}

type loadedMsg struct {
	cfg *ServersConfig
	err error
}

func (m rootModel) Init() tea.Cmd {
	return func() tea.Msg {
		cfg, err := LoadServers()
		return loadedMsg{cfg: cfg, err: err}
	}
}

func (m rootModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case loadedMsg:
		m.cfg = msg.cfg
		m.cfgErr = msg.err
		if m.cfg == nil {
			m.cfg = &ServersConfig{}
		}
		s := newServersModel(m.cfg, m.width, m.height)
		m.servers = &s
		return m, nil

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		if m.servers != nil {
			m.servers.resize(msg.Width, msg.Height)
		}
		if m.editor != nil {
			m.editor.resize(msg.Width, msg.Height)
		}
		if m.mods != nil {
			m.mods.resize(msg.Width, msg.Height)
		}
		return m, nil

	case tea.KeyMsg:
		// Global Ctrl+C always quits.
		if msg.String() == "ctrl+c" {
			m.quitting = true
			return m, tea.Quit
		}
	}

	// While the config is still loading, just eat messages.
	if m.cfg == nil {
		return m, nil
	}

	switch m.screen {
	case screenServers:
		return updateServers(m, msg)
	case screenEditor:
		return updateEditor(m, msg)
	case screenMods:
		return updateMods(m, msg)
	}
	return m, nil
}

func (m rootModel) View() string {
	if m.quitting {
		return ""
	}
	if m.cfg == nil {
		return "\n  Loading config…\n"
	}

	var body string
	switch m.screen {
	case screenServers:
		if m.servers != nil {
			body = m.servers.view()
		}
	case screenEditor:
		if m.editor != nil {
			body = m.editor.view()
		}
	case screenMods:
		if m.mods != nil {
			body = m.mods.view()
		}
	}

	status := m.lastMsg
	if m.cfgErr != nil {
		status = fmt.Sprintf("config error: %v", m.cfgErr)
	}
	if status != "" {
		body += "\n" + statusStyle.Render(status)
	}
	return body
}

var (
	titleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("229")).
			Background(lipgloss.Color("57")).
			Padding(0, 1).
			Bold(true)

	statusStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("244")).
			Italic(true)

	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("244"))

	errStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("196")).
			Bold(true)

	successStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("42")).
			Bold(true)

	warnStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("214"))

	mutedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("244"))
)

func main() {
	p := tea.NewProgram(rootModel{}, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "tui error: %v\n", err)
		os.Exit(1)
	}
}
