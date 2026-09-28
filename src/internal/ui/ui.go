// Package ui is the shared terminal-UX layer: colour and TTY detection,
// status output, and the check table.
//
// The primary caller of this binary is a GitHub Actions runner, not a person:
// five packs' sync-upstream.yml run it inside Docker every Monday. So
// non-interactive behaviour is the default case rather than a degraded one --
// no prompts unless stdin is a terminal, no ANSI into a log file, and a
// non-zero exit whenever something failed.
package ui

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/ichava/maintainer-toolkit/src/internal/core/checker"
	"github.com/mattn/go-isatty"
	"github.com/muesli/termenv"
)

// Config carries the resolved global flags.
type Config struct {
	AssumeYes bool
	NoInput   bool
	NoColor   bool
	JSON      bool
	Quiet     bool
	Out       io.Writer
	Err       io.Writer
}

var (
	assumeYes   bool
	noInput     bool
	jsonMode    bool
	quiet       bool
	colorOn     bool
	interactive bool
	out         io.Writer = os.Stdout
	errOut      io.Writer = os.Stderr
)

// Init configures the layer. Call once from the root command before any
// subcommand runs.
func Init(c Config) {
	assumeYes, noInput, jsonMode, quiet = c.AssumeYes, c.NoInput, c.JSON, c.Quiet

	out, errOut = os.Stdout, os.Stderr
	if c.Out != nil {
		out = c.Out
	}
	if c.Err != nil {
		errOut = c.Err
	}

	// NO_COLOR and TERM=dumb are honoured alongside the flag, per the CLI
	// guidelines. Colour is also off whenever stdout is not a terminal, which
	// is what keeps escape sequences out of a CI log.
	colorOn = !c.NoColor &&
		os.Getenv("NO_COLOR") == "" &&
		os.Getenv("TERM") != "dumb" &&
		isTerminal(os.Stdout)
	if !colorOn {
		lipgloss.SetColorProfile(termenv.Ascii)
	}

	// Interactivity keys off *stdin*, not stdout: a run whose output is piped
	// to a file can still be driven by a person, and one whose stdin is a pipe
	// cannot, however pretty its stdout is.
	interactive = !c.NoInput && isTerminal(os.Stdin) && !jsonMode
}

func isTerminal(f *os.File) bool {
	return isatty.IsTerminal(f.Fd()) || isatty.IsCygwinTerminal(f.Fd())
}

// Interactive reports whether prompting is allowed.
func Interactive() bool { return interactive }

// JSONMode reports whether output should be machine-readable.
func JSONMode() bool { return jsonMode }

// AssumeYes reports whether confirmations are pre-answered.
func AssumeYes() bool { return assumeYes }

// Colour styles. Each resolves to plain text when colour is off, because
// lipgloss has been switched to the ASCII profile.
var (
	styleBold    = lipgloss.NewStyle().Bold(true)
	styleDim     = lipgloss.NewStyle().Faint(true)
	styleOK      = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	styleWarn    = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	styleErr     = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	styleHeading = lipgloss.NewStyle().Bold(true).Underline(true)
)

// Bold, Dim, OK, Warn and Err style a string for the active profile.
func Bold(s string) string { return styleBold.Render(s) }
func Dim(s string) string  { return styleDim.Render(s) }
func OK(s string) string   { return styleOK.Render(s) }
func Warn(s string) string { return styleWarn.Render(s) }
func Err(s string) string  { return styleErr.Render(s) }

// Printf writes to stdout unless quiet.
func Printf(format string, a ...any) {
	if quiet {
		return
	}
	fmt.Fprintf(out, format, a...)
}

// Println writes a line to stdout unless quiet.
func Println(a ...any) {
	if quiet {
		return
	}
	fmt.Fprintln(out, a...)
}

// Status writes a status line to stderr.
//
// Status, progress and diagnostics go to stderr so stdout carries only the
// command's actual output -- which is what makes `imt check --json | jq` work.
func Status(glyph, format string, a ...any) {
	if quiet {
		return
	}
	fmt.Fprintf(errOut, "%s %s\n", glyph, fmt.Sprintf(format, a...))
}

// Success, Failure, Warning and Info are the four status shapes.
func Success(format string, a ...any) { Status(OK(Sym.Check), format, a...) }
func Failure(format string, a ...any) { Status(Err(Sym.Cross), format, a...) }
func Warning(format string, a ...any) { Status(Warn(Sym.Warn), format, a...) }
func Info(format string, a ...any)    { Status(Dim(Sym.Info), format, a...) }

// Heading writes a section heading.
func Heading(s string) {
	if quiet {
		return
	}
	fmt.Fprintf(out, "\n%s\n", styleHeading.Render(s))
}

// JSON writes a value as indented JSON to stdout.
func JSON(v any) error {
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// Table renders rows under headers, padded to the widest cell per column.
//
// Deliberately plain: no box drawing. This output is read as often in a CI log
// as in a terminal, and a bordered table is noise there.
func Table(headers []string, rows [][]string) string {
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = lipgloss.Width(h)
	}
	for _, row := range rows {
		for i, cell := range row {
			if i < len(widths) {
				if w := lipgloss.Width(cell); w > widths[i] {
					widths[i] = w
				}
			}
		}
	}

	var b strings.Builder
	writeRow := func(cells []string, style func(string) string) {
		for i, cell := range cells {
			if i >= len(widths) {
				continue
			}
			text := cell
			if style != nil {
				text = style(cell)
			}
			b.WriteString(text)
			if i < len(cells)-1 {
				// Pad by display width, not byte length: a status cell can
				// hold a multi-byte glyph, and len() would misalign the column
				// by exactly the number of continuation bytes.
				b.WriteString(strings.Repeat(" ", widths[i]-lipgloss.Width(cell)+2))
			}
		}
		b.WriteString("\n")
	}

	writeRow(headers, Bold)
	for _, row := range rows {
		writeRow(row, nil)
	}
	return b.String()
}

// CheckRow is one row of the upstream-check table, front-end agnostic.
type CheckRow struct {
	Package, Current, Latest, Status, Detail string
}

// RenderCheckTable renders the check results.
//
// It lives here rather than in either front-end so the CLI and the TUI show
// the same five columns in the same order; drift between them would be
// invisible until someone compared two screenshots.
func RenderCheckTable(rows []CheckRow) string {
	cells := make([][]string, 0, len(rows))
	for _, r := range rows {
		cells = append(cells, []string{r.Package, r.Current, r.Latest, r.Status, r.Detail})
	}
	return Table([]string{"Package", "Current", "Latest", "Status", "Detail"}, cells)
}

// CheckRows maps results to display rows, colouring the status per verdict.
//
// Both front-ends call this rather than mapping independently: a status glyph
// that differed between the CLI and the TUI would be invisible until someone
// compared two screenshots.
func CheckRows(results []checker.Result) []CheckRow {
	rows := make([]CheckRow, 0, len(results))
	for _, r := range results {
		var status string
		switch r.Status() {
		case checker.StatusOK:
			status = OK(Sym.Check + " ok")
		case checker.StatusUpdateAvailable:
			status = Warn(Sym.Warn + " update-available")
		default:
			status = Err(Sym.Cross + " error")
		}
		rows = append(rows, CheckRow{
			Package: r.Package, Current: orDash(r.Current), Latest: orDash(r.Latest),
			Status: status, Detail: r.Detail(),
		})
	}
	return rows
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// Summarise writes the one-line verdict under the check table.
//
// It reports unresolved packs separately from stale ones. Counting only stale
// packs means a run in which every registry was unreachable prints "All packs
// up to date" -- nothing was known to be behind, because nothing was looked
// at. That is the false green the Python reporter shipped.
func Summarise(results []checker.Result) {
	stale := checker.CountStale(results)
	errored := checker.CountErrored(results)

	switch {
	case errored == len(results) && errored > 0:
		Failure("no pack could be checked: %d of %d upstreams were unreachable.", errored, len(results))
	case errored > 0 && stale > 0:
		Warning("%d pack(s) behind upstream; %d could not be checked.", stale, errored)
	case errored > 0:
		Warning("%d pack(s) up to date; %d could not be checked.", len(results)-errored, errored)
	case stale > 0:
		Warning("%d pack(s) behind upstream.", stale)
	default:
		Success("All packs up to date.")
	}
}
