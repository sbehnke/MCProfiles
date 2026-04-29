//go:build headless

package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// modItem wraps a ModInfo for display in a bubbles/list.
type modItem struct {
	mod      ModInfo
	updating bool
	updated  bool
	failed   string // non-empty => update failed with this reason
}

func (i modItem) FilterValue() string { return i.mod.Filename }

func (i modItem) Title() string {
	name := i.mod.ProjectTitle
	if name == "" {
		name = i.mod.Filename
	}
	switch {
	case i.updating:
		return "⋯ " + name
	case i.updated:
		return successStyle.Render("✓") + " " + name
	case i.failed != "":
		return errStyle.Render("✗") + " " + name
	case !i.mod.Found:
		return mutedStyle.Render("?") + " " + name
	case i.mod.HasUpdate:
		return warnStyle.Render("↑") + " " + name
	default:
		return successStyle.Render("=") + " " + name
	}
}

func (i modItem) Description() string {
	switch {
	case i.updating:
		return mutedStyle.Render("updating…")
	case i.updated:
		return successStyle.Render("updated to " + i.mod.CurrentVersion)
	case i.failed != "":
		return errStyle.Render(i.failed)
	case !i.mod.Found:
		if i.mod.JarMeta != nil && i.mod.JarMeta.Version != "" {
			return mutedStyle.Render(i.mod.JarMeta.Version + " · not on Modrinth")
		}
		return mutedStyle.Render("not on Modrinth · " + i.mod.Filename)
	case i.mod.HasUpdate:
		return warnStyle.Render(fmt.Sprintf("%s → %s", i.mod.CurrentVersion, i.mod.LatestVersion))
	default:
		return mutedStyle.Render(i.mod.CurrentVersion + " · up to date")
	}
}

type modsModel struct {
	server    Server
	list      list.Model
	spinner   spinner.Model
	loading   bool
	checkErr  error
	summary   string
	width     int
	height    int
	inflight  int // number of in-progress update tasks
	updateLog []string
}

func newModsModel(srv Server, w, h int) modsModel {
	delegate := list.NewDefaultDelegate()
	lw, lh := listDims(w, h)
	l := list.New(nil, delegate, lw, lh)
	l.Title = "Mods: " + srv.Name
	l.Styles.Title = titleStyle
	l.SetStatusBarItemName("mod", "mods")
	l.AdditionalShortHelpKeys = modListHelpKeys
	l.AdditionalFullHelpKeys = modListHelpKeys

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))

	return modsModel{
		server:  srv,
		list:    l,
		spinner: sp,
		loading: true,
	}
}

func modListHelpKeys() []key.Binding {
	return []key.Binding{
		key.NewBinding(key.WithKeys("u"), key.WithHelp("u", "update")),
		key.NewBinding(key.WithKeys("U"), key.WithHelp("U", "update all")),
		key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
		key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
		key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit")),
	}
}

func (m *modsModel) resize(w, h int) {
	m.width, m.height = w, h
	lw, lh := listDims(w, h)
	m.list.SetSize(lw, lh)
}

// --- messages ---------------------------------------------------------------

type modsLoadedMsg struct {
	mods        []ModInfo
	err         error
	gameVersion string
}

type modUpdatedMsg struct {
	index int
	newFn string
	newVN string
	err   error
}

// startCheck kicks off CheckMods and returns the tea.Cmd that produces modsLoadedMsg.
func (m *modsModel) startCheck() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		func() tea.Msg {
			gv := m.server.GameVersion
			if gv == "" && m.server.ServerJar != "" {
				if info, err := DetectServerJar(m.server.ServerJar); err == nil {
					gv = info.GameVersion
				}
			}
			mods, err := CheckMods(m.server.ModsDir, gv)
			return modsLoadedMsg{mods: mods, err: err, gameVersion: gv}
		},
	)
}

func sortMods(mods []ModInfo) []ModInfo {
	sort.Slice(mods, func(i, j int) bool {
		a, b := mods[i], mods[j]
		if a.HasUpdate != b.HasUpdate {
			return a.HasUpdate
		}
		if a.Found != b.Found {
			return a.Found
		}
		ai := a.ProjectTitle
		if ai == "" {
			ai = a.Filename
		}
		bi := b.ProjectTitle
		if bi == "" {
			bi = b.Filename
		}
		return strings.ToLower(ai) < strings.ToLower(bi)
	})
	return mods
}

func buildModItems(mods []ModInfo) []list.Item {
	items := make([]list.Item, len(mods))
	for i, m := range mods {
		items[i] = modItem{mod: m}
	}
	return items
}

func modSummary(mods []ModInfo) string {
	var foundCount, updateCount, unknownCount int
	for _, mm := range mods {
		switch {
		case !mm.Found:
			unknownCount++
		case mm.HasUpdate:
			updateCount++
		default:
			foundCount++
		}
	}
	return fmt.Sprintf("%d mods · %d up to date · %d updates · %d unknown",
		len(mods), foundCount, updateCount, unknownCount)
}

// selectedMod returns a copy of the selected modItem and its index.
func (m modsModel) selectedMod() (modItem, int, bool) {
	idx := m.list.Index()
	items := m.list.Items()
	if idx < 0 || idx >= len(items) {
		return modItem{}, -1, false
	}
	mi, ok := items[idx].(modItem)
	if !ok {
		return modItem{}, -1, false
	}
	return mi, idx, true
}

func (m *modsModel) setItem(idx int, mi modItem) {
	m.list.SetItem(idx, mi)
}

// updateMod downloads the update for item at idx.
func (m *modsModel) updateMod(idx int) tea.Cmd {
	items := m.list.Items()
	if idx < 0 || idx >= len(items) {
		return nil
	}
	mi, ok := items[idx].(modItem)
	if !ok || !mi.mod.HasUpdate || mi.mod.UpdateURL == "" {
		return nil
	}
	mi.updating = true
	m.setItem(idx, mi)
	m.inflight++

	modCopy := mi.mod
	return func() tea.Msg {
		newPath, err := DownloadMod(modCopy)
		if err != nil {
			return modUpdatedMsg{index: idx, err: err}
		}
		return modUpdatedMsg{
			index: idx,
			newFn: filepathBase(newPath),
			newVN: modCopy.LatestVersion,
		}
	}
}

func filepathBase(p string) string {
	i := strings.LastIndexAny(p, "/\\")
	if i < 0 {
		return p
	}
	return p[i+1:]
}

func (m modsModel) view() string {
	if m.loading {
		return fmt.Sprintf("\n  %s Scanning %s…\n\n  %s\n",
			m.spinner.View(), m.server.ModsDir,
			helpStyle.Render("esc: back · q: quit"))
	}
	if m.checkErr != nil {
		return fmt.Sprintf("\n  %s\n\n  %s\n",
			errStyle.Render("check failed: "+m.checkErr.Error()),
			helpStyle.Render("esc: back · q: quit"))
	}

	body := m.list.View()
	bottom := mutedStyle.Render(m.summary)
	if m.inflight > 0 {
		bottom += "   " + m.spinner.View() + " " +
			mutedStyle.Render(fmt.Sprintf("%d updates in progress", m.inflight))
	}
	return body + "\n" + bottom
}

func updateMods(m rootModel, msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case modsLoadedMsg:
		m.mods.loading = false
		if msg.err != nil {
			m.mods.checkErr = msg.err
			return m, nil
		}
		sorted := sortMods(msg.mods)
		m.mods.list.SetItems(buildModItems(sorted))
		summary := modSummary(sorted)
		switch {
		case msg.gameVersion == "":
			summary += "  ·  " + warnStyle.Render("no game_version set — update detection skipped")
		case m.mods.server.GameVersion == "":
			summary += "  ·  " + mutedStyle.Render("game_version "+msg.gameVersion+" (auto-detected)")
		}
		m.mods.summary = summary
		return m, nil

	case modUpdatedMsg:
		items := m.mods.list.Items()
		if msg.index >= 0 && msg.index < len(items) {
			if mi, ok := items[msg.index].(modItem); ok {
				mi.updating = false
				if msg.err != nil {
					mi.failed = msg.err.Error()
				} else {
					mi.updated = true
					mi.mod.CurrentVersion = msg.newVN
					mi.mod.HasUpdate = false
					mi.mod.Filename = msg.newFn
				}
				m.mods.setItem(msg.index, mi)
			}
		}
		if m.mods.inflight > 0 {
			m.mods.inflight--
		}
		// Rebuild summary from current items.
		items = m.mods.list.Items()
		mods := make([]ModInfo, 0, len(items))
		for _, it := range items {
			if mi, ok := it.(modItem); ok {
				mods = append(mods, mi.mod)
			}
		}
		m.mods.summary = modSummary(mods)
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.mods.spinner, cmd = m.mods.spinner.Update(msg)
		if m.mods.loading || m.mods.inflight > 0 {
			return m, cmd
		}
		return m, nil

	case tea.KeyMsg:
		if m.mods.list.FilterState() == list.Filtering {
			break
		}
		switch msg.String() {
		case "esc":
			m.screen = screenServers
			m.lastMsg = ""
			return m, nil
		case "q":
			m.quitting = true
			return m, tea.Quit
		case "r":
			m.mods.loading = true
			m.mods.checkErr = nil
			m.mods.list.SetItems(nil)
			return m, m.mods.startCheck()
		case "u":
			_, idx, ok := m.mods.selectedMod()
			if !ok {
				return m, nil
			}
			return m, m.mods.updateMod(idx)
		case "U":
			var cmds []tea.Cmd
			for i, it := range m.mods.list.Items() {
				if mi, ok := it.(modItem); ok && mi.mod.HasUpdate && !mi.updating && !mi.updated {
					cmds = append(cmds, m.mods.updateMod(i))
				}
			}
			if len(cmds) == 0 {
				m.lastMsg = "no updates available"
				return m, nil
			}
			cmds = append(cmds, m.mods.spinner.Tick)
			return m, tea.Batch(cmds...)
		}
	}

	var cmd tea.Cmd
	m.mods.list, cmd = m.mods.list.Update(msg)
	return m, cmd
}
