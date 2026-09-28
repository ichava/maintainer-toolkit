// Package tui is the interactive front-end.
//
// It is a front-end, not a second implementation: every screen calls the same
// internal/app orchestration and internal/ui rendering the commands do, so a
// behaviour exists in both or neither. Those two packages exist precisely
// because the first attempt kept the shared helpers in internal/cli, which
// deadlocked into an import cycle the moment the CLI needed to launch the TUI.
package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/ichava/maintainer-toolkit/src/internal/app"
	"github.com/ichava/maintainer-toolkit/src/internal/core/checker"
	"github.com/ichava/maintainer-toolkit/src/internal/core/config"
	"github.com/ichava/maintainer-toolkit/src/internal/ui"
	"github.com/ichava/maintainer-toolkit/src/internal/version"
)

type screen int

const (
	screenMenu screen = iota
	screenChecking
	screenResults
	screenPacks
	screenDetail
)

type menuItem struct {
	label  string
	detail string
	action func(*Model) (tea.Model, tea.Cmd)
}

// Model is the root model and the screen router.
type Model struct {
	configDir string
	screen    screen

	cursor   int
	items    []menuItem
	spin     spinner.Model
	width    int
	height   int
	quitting bool

	packs   []*config.PackConfig
	results []checker.Result
	err     error

	packCursor int
	selected   *config.PackConfig
}

// New builds the root model.
func New(configDir string) *Model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("4"))

	m := &Model{configDir: configDir, spin: s, width: 80}
	m.items = []menuItem{
		{"Check upstream status (all packs)", "poll every registry and compare against what each pack vendors",
			func(m *Model) (tea.Model, tea.Cmd) { return m, m.startCheck("") }},
		{"Check upstream status (single pack)", "pick one pack, then poll just that registry",
			func(m *Model) (tea.Model, tea.Cmd) { m.screen = screenPacks; return m, m.loadPacks() }},
		{"List registered packs", "the registry, with the version each pack repo records",
			func(m *Model) (tea.Model, tea.Cmd) { m.screen = screenPacks; return m, m.loadPacks() }},
		{"Quit", "", func(m *Model) (tea.Model, tea.Cmd) { m.quitting = true; return m, tea.Quit }},
	}
	return m
}

// Init starts the spinner.
func (m *Model) Init() tea.Cmd { return m.spin.Tick }

// --- messages ---

type packsLoadedMsg struct {
	packs []*config.PackConfig
	err   error
}

type checkDoneMsg struct {
	results []checker.Result
	err     error
}

func (m *Model) loadPacks() tea.Cmd {
	dir := m.configDir
	return func() tea.Msg {
		packs, err := app.LoadPacks(dir, "")
		return packsLoadedMsg{packs: packs, err: err}
	}
}

// startCheck runs the same CheckAll the `check` command does.
func (m *Model) startCheck(only string) tea.Cmd {
	m.screen = screenChecking
	m.err = nil
	dir := m.configDir

	return tea.Batch(m.spin.Tick, func() tea.Msg {
		packs, err := app.LoadPacks(dir, only)
		if err != nil {
			return checkDoneMsg{err: err}
		}
		// A registry can be slow; the timeout keeps the TUI from looking hung
		// with no way out.
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		return checkDoneMsg{results: app.CheckAll(ctx, packs)}
	})
}

// Update handles input and messages.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case packsLoadedMsg:
		m.packs, m.err = msg.packs, msg.err
		m.packCursor = 0
		return m, nil

	case checkDoneMsg:
		m.results, m.err = msg.results, msg.err
		m.screen = screenResults
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		// Ctrl-C in the alt-screen never reaches the OS as SIGINT, because raw
		// mode swallows it -- so it has to be handled here or the only way out
		// is another terminal.
		m.quitting = true
		return m, tea.Quit

	case "esc":
		switch m.screen {
		case screenMenu:
			m.quitting = true
			return m, tea.Quit
		case screenDetail:
			m.screen = screenPacks
		default:
			m.screen = screenMenu
		}
		return m, nil

	case "up", "k":
		m.moveCursor(-1)
		return m, nil

	case "down", "j":
		m.moveCursor(1)
		return m, nil

	case "enter":
		return m.activate()

	case "r":
		if m.screen == screenResults {
			return m, m.startCheck("")
		}
	}
	return m, nil
}

func (m *Model) moveCursor(delta int) {
	switch m.screen {
	case screenMenu:
		m.cursor = clamp(m.cursor+delta, 0, len(m.items)-1)
	case screenPacks:
		m.packCursor = clamp(m.packCursor+delta, 0, len(m.packs)-1)
	}
}

func (m *Model) activate() (tea.Model, tea.Cmd) {
	switch m.screen {
	case screenMenu:
		if m.cursor < len(m.items) {
			return m.items[m.cursor].action(m)
		}
	case screenPacks:
		if m.packCursor < len(m.packs) {
			m.selected = m.packs[m.packCursor]
			m.screen = screenDetail
		}
	case screenDetail:
		if m.selected != nil {
			return m, m.startCheck(m.selected.Name)
		}
	case screenResults:
		m.screen = screenMenu
	}
	return m, nil
}

func clamp(v, lo, hi int) int {
	if hi < lo {
		return lo
	}
	if v < lo {
		return hi // wrap, so a long list does not dead-end at either edge
	}
	if v > hi {
		return lo
	}
	return v
}

// --- view ---

var (
	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
	subtitleStyle = lipgloss.NewStyle().Faint(true)
	cursorStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("5"))
	helpStyle     = lipgloss.NewStyle().Faint(true)
	errStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
)

// View renders the current screen.
func (m *Model) View() string {
	if m.quitting {
		return ""
	}

	var b strings.Builder
	b.WriteString(titleStyle.Render(fmt.Sprintf("%s %s", version.Name, version.Version())))
	b.WriteString("\n")
	b.WriteString(subtitleStyle.Render("config: " + m.configDir))
	b.WriteString("\n\n")

	switch m.screen {
	case screenMenu:
		b.WriteString(m.viewMenu())
	case screenChecking:
		b.WriteString(fmt.Sprintf("%s polling upstream registries%s\n", m.spin.View(), ui.Sym.Ellipsis))
	case screenResults:
		b.WriteString(m.viewResults())
	case screenPacks:
		b.WriteString(m.viewPacks())
	case screenDetail:
		b.WriteString(m.viewDetail())
	}

	if m.err != nil {
		b.WriteString("\n" + errStyle.Render(ui.Sym.Cross+" "+m.err.Error()) + "\n")
	}
	b.WriteString("\n" + helpStyle.Render(m.help()))
	return b.String()
}

func (m *Model) viewMenu() string {
	var b strings.Builder
	for i, item := range m.items {
		prefix := "  "
		label := item.label
		if i == m.cursor {
			prefix = cursorStyle.Render(ui.Sym.Cursor + " ")
			label = cursorStyle.Render(label)
		}
		b.WriteString(prefix + label + "\n")
		if i == m.cursor && item.detail != "" {
			b.WriteString(subtitleStyle.Render("    "+item.detail) + "\n")
		}
	}
	return b.String()
}

func (m *Model) viewResults() string {
	if len(m.results) == 0 {
		return subtitleStyle.Render("no results\n")
	}
	// The same renderer the `check` command uses, so the two front-ends cannot
	// drift into showing different columns.
	out := ui.RenderCheckTable(ui.CheckRows(m.results))
	if n := checker.CountStale(m.results); n > 0 {
		out += "\n" + lipgloss.NewStyle().Foreground(lipgloss.Color("3")).
			Render(fmt.Sprintf("%s %d pack(s) behind upstream.", ui.Sym.Warn, n)) + "\n"
	} else {
		out += "\n" + lipgloss.NewStyle().Foreground(lipgloss.Color("2")).
			Render(ui.Sym.Check+" All packs up to date.") + "\n"
	}
	return out
}

func (m *Model) viewPacks() string {
	if len(m.packs) == 0 {
		return subtitleStyle.Render("loading" + ui.Sym.Ellipsis + "\n")
	}
	var b strings.Builder
	for i, p := range m.packs {
		prefix := "  "
		line := fmt.Sprintf("%-26s %s", p.Name, subtitleStyle.Render(p.Pack))
		if i == m.packCursor {
			prefix = cursorStyle.Render(ui.Sym.Cursor + " ")
			line = fmt.Sprintf("%-26s %s", cursorStyle.Render(p.Name), subtitleStyle.Render(p.Pack))
		}
		b.WriteString(prefix + line + "\n")
	}
	return b.String()
}

func (m *Model) viewDetail() string {
	p := m.selected
	if p == nil {
		return ""
	}
	rows := [][]string{
		{"composer", p.Pack},
		{"pack root", p.PackRoot},
		{"vendored", orDash(config.ReadVendoredVersion(p))},
		{"config fallback", orDash(p.CurrentVersion)},
		{"resolved", orDash(config.ResolvedCurrentVersion(p))},
		{"source", p.Source.Type},
		{"check url", p.VersionCheckURL},
		{"version file", p.VersionFile},
		{"sinks", fmt.Sprintf("%d", len(p.Sinks))},
	}
	var b strings.Builder
	b.WriteString(titleStyle.Render(p.Name) + "\n\n")
	for _, r := range rows {
		b.WriteString(fmt.Sprintf("  %-16s %s\n", subtitleStyle.Render(r[0]), r[1]))
	}
	return b.String()
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func (m *Model) help() string {
	switch m.screen {
	case screenMenu:
		return "up/down move  enter select  q quit"
	case screenChecking:
		return "q cancel"
	case screenResults:
		return "r re-check  enter back  q quit"
	case screenPacks:
		return "up/down move  enter open  esc back  q quit"
	case screenDetail:
		return "enter check this pack  esc back  q quit"
	}
	return "q quit"
}

// Run starts the TUI.
func Run(configDir string) error {
	p := tea.NewProgram(New(configDir), tea.WithAltScreen())
	_, err := p.Run()
	return err
}
