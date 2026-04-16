//go:build headless

package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// --- server list screen ------------------------------------------------------

type serverItem struct {
	server Server
}

func (i serverItem) FilterValue() string { return i.server.Name }
func (i serverItem) Title() string       { return i.server.Name }
func (i serverItem) Description() string {
	parts := []string{}
	if i.server.GameVersion != "" {
		parts = append(parts, i.server.GameVersion)
	}
	if i.server.Loader != "" {
		parts = append(parts, i.server.Loader)
	}
	if i.server.ModsDir != "" {
		parts = append(parts, i.server.ModsDir)
	}
	return strings.Join(parts, " · ")
}

type serversModel struct {
	list list.Model
}

func newServersModel(cfg *ServersConfig, w, h int) serversModel {
	items := make([]list.Item, 0, len(cfg.Servers))
	for _, s := range cfg.Servers {
		items = append(items, serverItem{server: s})
	}
	delegate := list.NewDefaultDelegate()
	lw, lh := listDims(w, h)
	l := list.New(items, delegate, lw, lh)
	l.Title = "Servers"
	l.SetStatusBarItemName("server", "servers")
	l.Styles.Title = titleStyle
	l.AdditionalShortHelpKeys = serverListHelpKeys
	l.AdditionalFullHelpKeys = serverListHelpKeys
	return serversModel{list: l}
}

func serverListHelpKeys() []key.Binding {
	return []key.Binding{
		key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open")),
		key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "add")),
		key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "edit")),
		key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "delete")),
		key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit")),
	}
}

func (s *serversModel) resize(w, h int) {
	lw, lh := listDims(w, h)
	s.list.SetSize(lw, lh)
}

// listDims returns safe (w, h) for a list.Model, accounting for the title/status
// bar and flooring at sane minima so pre-WindowSizeMsg zero values don't panic.
func listDims(w, h int) (int, int) {
	if w < 20 {
		w = 20
	}
	h -= 4
	if h < 3 {
		h = 3
	}
	return w, h
}

func (s *serversModel) refresh(cfg *ServersConfig) {
	items := make([]list.Item, 0, len(cfg.Servers))
	for _, srv := range cfg.Servers {
		items = append(items, serverItem{server: srv})
	}
	s.list.SetItems(items)
}

func (s serversModel) view() string { return s.list.View() }

func (s serversModel) selected() (Server, int, bool) {
	item, ok := s.list.SelectedItem().(serverItem)
	if !ok {
		return Server{}, -1, false
	}
	return item.server, s.list.Index(), true
}

func updateServers(m rootModel, msg tea.Msg) (tea.Model, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok {
		// Bypass list filtering when filter isn't active.
		if m.servers.list.FilterState() != list.Filtering {
			switch km.String() {
			case "q":
				m.quitting = true
				return m, tea.Quit
			case "a":
				e := newEditorModel(Server{}, -1, m.width, m.height)
				m.editor = &e
				m.screen = screenEditor
				m.lastMsg = ""
				return m, m.editor.input[0].Focus()
			case "e":
				srv, idx, ok := m.servers.selected()
				if !ok {
					return m, nil
				}
				e := newEditorModel(srv, idx, m.width, m.height)
				m.editor = &e
				m.screen = screenEditor
				m.lastMsg = ""
				return m, m.editor.input[0].Focus()
			case "d":
				_, idx, ok := m.servers.selected()
				if !ok {
					return m, nil
				}
				m.cfg.Servers = append(m.cfg.Servers[:idx], m.cfg.Servers[idx+1:]...)
				if err := SaveServers(m.cfg); err != nil {
					m.lastMsg = errStyle.Render("delete failed: " + err.Error())
				} else {
					m.lastMsg = "server deleted"
				}
				m.servers.refresh(m.cfg)
				return m, nil
			case "enter":
				srv, _, ok := m.servers.selected()
				if !ok {
					return m, nil
				}
				if srv.ModsDir == "" {
					m.lastMsg = warnStyle.Render("this server has no mods_dir — edit it first")
					return m, nil
				}
				md := newModsModel(srv, m.width, m.height)
				m.mods = &md
				m.screen = screenMods
				m.lastMsg = ""
				return m, m.mods.startCheck()
			}
		}
	}

	var cmd tea.Cmd
	m.servers.list, cmd = m.servers.list.Update(msg)
	return m, cmd
}

// --- editor screen -----------------------------------------------------------

const (
	fieldName = iota
	fieldModsDir
	fieldServerJar
	fieldGameVersion
	fieldLoader
	fieldJavaPath
	fieldCount
)

var fieldLabels = []string{
	"Name",
	"Mods folder",
	"Server jar (optional)",
	"Game version",
	"Loader (fabric/forge/neoforge/quilt/paper/vanilla)",
	"Java path (optional)",
}

type editorModel struct {
	input        [fieldCount]textinput.Model
	focus        int
	originalIdx  int // -1 = new entry
	detectStatus string
	width        int
	height       int
}

func newEditorModel(srv Server, idx, w, h int) editorModel {
	m := editorModel{originalIdx: idx, width: w, height: h}
	values := []string{srv.Name, srv.ModsDir, srv.ServerJar, srv.GameVersion, srv.Loader, srv.JavaPath}
	for i := 0; i < fieldCount; i++ {
		ti := textinput.New()
		ti.Prompt = "  "
		ti.Width = 60
		ti.CharLimit = 512
		ti.SetValue(values[i])
		m.input[i] = ti
	}
	m.input[0].Focus()
	return m
}

func (e *editorModel) resize(w, h int) {
	e.width, e.height = w, h
	width := w - 6
	if width < 20 {
		width = 20
	}
	for i := range e.input {
		e.input[i].Width = width
	}
}

func (e editorModel) toServer() Server {
	return Server{
		Name:        strings.TrimSpace(e.input[fieldName].Value()),
		ModsDir:     strings.TrimSpace(e.input[fieldModsDir].Value()),
		ServerJar:   strings.TrimSpace(e.input[fieldServerJar].Value()),
		GameVersion: strings.TrimSpace(e.input[fieldGameVersion].Value()),
		Loader:      strings.TrimSpace(e.input[fieldLoader].Value()),
		JavaPath:    strings.TrimSpace(e.input[fieldJavaPath].Value()),
	}
}

func (e editorModel) view() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render(" Edit Server ") + "\n\n")
	for i := 0; i < fieldCount; i++ {
		label := fieldLabels[i]
		if i == e.focus {
			label = "> " + label
		} else {
			label = "  " + label
		}
		b.WriteString(lipgloss.NewStyle().Bold(i == e.focus).Render(label))
		b.WriteString("\n")
		b.WriteString(e.input[i].View())
		b.WriteString("\n\n")
	}
	if e.detectStatus != "" {
		b.WriteString(e.detectStatus + "\n\n")
	}
	b.WriteString(helpStyle.Render("tab: next · shift+tab: prev · ctrl+d: detect from server jar · ctrl+s: save · esc: cancel"))
	return b.String()
}

func (e *editorModel) cycleFocus(dir int) tea.Cmd {
	e.input[e.focus].Blur()
	e.focus = (e.focus + dir + fieldCount) % fieldCount
	return e.input[e.focus].Focus()
}

type detectedMsg struct {
	info ServerJarInfo
	err  error
}

func (e editorModel) detect() tea.Cmd {
	path := strings.TrimSpace(e.input[fieldServerJar].Value())
	return func() tea.Msg {
		if path == "" {
			return detectedMsg{err: fmt.Errorf("no server jar set")}
		}
		info, err := DetectServerJar(path)
		return detectedMsg{info: info, err: err}
	}
}

func updateEditor(m rootModel, msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case detectedMsg:
		if msg.err != nil {
			m.editor.detectStatus = errStyle.Render("detect: " + msg.err.Error())
			return m, nil
		}
		changed := []string{}
		if msg.info.GameVersion != "" {
			m.editor.input[fieldGameVersion].SetValue(msg.info.GameVersion)
			changed = append(changed, "game_version="+msg.info.GameVersion)
		}
		if msg.info.Loader != "" {
			m.editor.input[fieldLoader].SetValue(msg.info.Loader)
			changed = append(changed, "loader="+msg.info.Loader)
		}
		if len(changed) == 0 {
			m.editor.detectStatus = warnStyle.Render("detect: no metadata found in jar")
		} else {
			m.editor.detectStatus = successStyle.Render("detected: " + strings.Join(changed, ", "))
		}
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			m.screen = screenServers
			m.lastMsg = ""
			return m, nil
		case "tab", "down":
			cmd := m.editor.cycleFocus(1)
			return m, cmd
		case "shift+tab", "up":
			cmd := m.editor.cycleFocus(-1)
			return m, cmd
		case "ctrl+d":
			return m, m.editor.detect()
		case "ctrl+s":
			srv := m.editor.toServer()
			if srv.Name == "" || srv.ModsDir == "" {
				m.editor.detectStatus = errStyle.Render("name and mods_dir are required")
				return m, nil
			}
			if m.editor.originalIdx >= 0 && m.editor.originalIdx < len(m.cfg.Servers) {
				m.cfg.Servers[m.editor.originalIdx] = srv
			} else {
				m.cfg.Servers = append(m.cfg.Servers, srv)
			}
			if err := SaveServers(m.cfg); err != nil {
				m.editor.detectStatus = errStyle.Render("save failed: " + err.Error())
				return m, nil
			}
			m.servers.refresh(m.cfg)
			m.screen = screenServers
			m.lastMsg = successStyle.Render("saved " + srv.Name)
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.editor.input[m.editor.focus], cmd = m.editor.input[m.editor.focus].Update(msg)
	return m, cmd
}
