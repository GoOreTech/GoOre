// Package inspect — Bubble Tea model. Pure(ish) state: Update returns a new model + an optional command. Read-only — no in-place mutation. See docs/inspect.md.
package inspect

import (
	"fmt"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Model is the Bubble Tea model for the world inspector TUI.
type Model struct {
	world         *WorldData
	players       list.Model
	details       viewport.Model
	helpVisible   bool
	width, height int
	corruptCount  int
	statusMsg     string
}

// NewModel creates a Model from a WorldData snapshot. The caller retains ownership; the model only reads it.
func NewModel(wd *WorldData) *Model {
	if wd == nil {
		panic("inspect.NewModel: WorldData is nil")
	}
	items := make([]list.Item, len(wd.Players))
	for i, p := range wd.Players {
		items[i] = playerListItem{info: p}
	}
	playersList := list.New(items, list.NewDefaultDelegate(), 30, 20)
	playersList.Title = "Players"
	playersList.SetShowStatusBar(false)
	playersList.SetFilteringEnabled(false)
	playersList.SetShowHelp(false)

	vp := viewport.New(50, 20)

	m := &Model{
		world:        wd,
		players:      playersList,
		details:      vp,
		corruptCount: len(wd.Errors),
	}
	m.refreshDetails()
	return m
}

// Init returns the initial tea.Cmd (none — we're event-driven).
func (m *Model) Init() tea.Cmd { return nil }

// Selected returns the index of the currently selected player.
// Used by tests.
func (m *Model) Selected() int { return m.players.Index() }

// HelpVisible returns whether the help overlay is currently shown.
// Used by tests.
func (m *Model) HelpVisible() bool { return m.helpVisible }

// Players returns the underlying list model. Used by tests.
func (m *Model) Players() list.Model { return m.players }

// Update is the Bubble Tea message handler.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		// 30 cols for list, rest for details. Narrow screens: 20.
		listW := 30
		if m.width < 80 {
			listW = 20
		}
		m.players.SetSize(listW, m.height-4) // -2 header, -2 footer
		m.details.Width = m.width - listW - 2
		m.details.Height = m.height - 4
		m.refreshDetails()
		return m, nil

	case tea.KeyMsg:
		if m.helpVisible {
			switch msg.String() {
			case "?", "esc", "q":
				m.helpVisible = false
			}
			return m, nil
		}
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "?":
			m.helpVisible = true
			return m, nil
		case "r":
			return m, m.reloadCmd()
		case "down", "j":
			m.players.CursorDown()
			m.refreshDetails()
			return m, nil
		case "up", "k":
			m.players.CursorUp()
			m.refreshDetails()
			return m, nil
		case "home", "g":
			m.players.Select(0)
			m.refreshDetails()
			return m, nil
		case "end", "G":
			m.players.Select(len(m.world.Players) - 1)
			m.refreshDetails()
			return m, nil
		case "pgdown":
			m.players.CursorDown()
			m.players.CursorDown()
			m.players.CursorDown()
			m.refreshDetails()
			return m, nil
		case "pgup":
			m.players.CursorUp()
			m.players.CursorUp()
			m.players.CursorUp()
			m.refreshDetails()
			return m, nil
		}
	}

	// Pass through to list.
	var cmd tea.Cmd
	m.players, cmd = m.players.Update(msg)
	m.refreshDetails()
	return m, cmd
}

// View renders the TUI as a single string.
func (m *Model) View() string {
	if m.width == 0 {
		return "Initializing..."
	}
	if m.helpVisible {
		return m.viewHelp()
	}
	header := m.viewHeader()
	body := lipgloss.JoinHorizontal(lipgloss.Top,
		m.players.View(),
		m.details.View(),
	)
	footer := m.viewFooter()
	return lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
}

// viewHeader renders the top bar with seed and counts.
func (m *Model) viewHeader() string {
	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#FFFFFF")).
		Background(lipgloss.Color("#5F5FAF")).
		Padding(0, 1).
		Width(m.width)
	corruptStr := ""
	if m.corruptCount > 0 {
		corruptStr = fmt.Sprintf("  (%d corrupt)", m.corruptCount)
	}
	chunkMB := float64(m.world.ChunkBytes) / (1024 * 1024)
	chunkStr := fmt.Sprintf("Chunks %d (%.2f MB)", m.world.ChunkCount, chunkMB)
	left := fmt.Sprintf(" Seed %d  Players %d%s",
		m.world.Seed, len(m.world.Players), corruptStr)
	return headerStyle.Render(left + "  " + chunkStr)
}

// viewFooter renders the bottom bar with keybindings and status.
func (m *Model) viewFooter() string {
	footerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#888888")).
		Width(m.width)
	keys := "↑↓ nav  PgUp/PgDn page  Home/End first/last  r refresh  ? help  q quit"
	if m.statusMsg != "" {
		return footerStyle.Render(keys + "  |  " + m.statusMsg)
	}
	return footerStyle.Render(keys)
}

// viewHelp renders the help overlay.
func (m *Model) viewHelp() string {
	helpStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#5F5FAF")).
		Padding(1, 2).
		Width(m.width - 4)
	text := `goore inspect — keybindings

  ↑ / ↓      navigate player list
  j / k      navigate (vim-style)
  PgUp/PgDn  page through list
  Home/End   first / last player
  g / G      first / last (vim-style)
  r          refresh from disk
  ?          toggle this help
  q / Ctrl+C quit
  Esc        close this help

Mouse: click on a player name to select.

Press ? or Esc to close.`
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, helpStyle.Render(text))
}

// refreshDetails re-renders the right pane from the currently selected player.
func (m *Model) refreshDetails() {
	idx := m.players.Index()
	if idx < 0 || idx >= len(m.world.Players) {
		m.details.SetContent("(no player selected)")
		return
	}
	p := m.world.Players[idx]
	m.details.SetContent(renderPlayer(p))
}

// reloadCmd re-reads the world directory and returns a tea.Cmd that produces a reloadedMsg with the new WorldData.
func (m *Model) reloadCmd() tea.Cmd {
	dir := m.world.Dir
	return func() tea.Msg {
		wd, err := LoadWorld(dir)
		if err != nil {
			return reloadErrMsg{err: err}
		}
		return reloadedMsg{wd: wd}
	}
}

type reloadedMsg struct {
	wd *WorldData
}

type reloadErrMsg struct {
	err error
}
