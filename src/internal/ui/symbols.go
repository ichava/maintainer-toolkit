package ui

import (
	"os"
	"runtime"
)

// Glyphs is the display symbol set, chosen once for the running terminal.
//
// Legacy Windows consoles (bare cmd.exe, old conhost) have no Unicode font and
// render box-drawing and status glyphs as "?" or empty boxes. Everywhere else
// -- macOS, Linux, WSL, Windows Terminal, VS Code -- gets the Unicode set.
//
// The status vocabulary is the terminal handbook's: one symbol per bucket, so
// a reader learns the mental model once. The ASCII column is its documented
// fallback set.
type Glyphs struct {
	Check, Cross, Warn, Info string
	Active, Pending, Paused  string
	Retrying, Skipped        string
	Cursor, Bullet           string
	Dash, Ellipsis           string
	Arrow, Branch, Corner    string
	BarFull, BarEmpty        string
}

// GlyphsUnicode and GlyphsASCII are exported so a golden test can pin one and
// produce identical output regardless of the host.
var (
	GlyphsUnicode = Glyphs{
		Check: "✓", Cross: "✗", Warn: "⚠", Info: "ℹ",
		Active: "◉", Pending: "○", Paused: "⏸",
		Retrying: "↻", Skipped: "⊘",
		Cursor: "▸", Bullet: "•",
		Dash: "—", Ellipsis: "…",
		Arrow: "→", Branch: "├─", Corner: "└─",
		BarFull: "█", BarEmpty: "░",
	}
	GlyphsASCII = Glyphs{
		Check: "[OK]", Cross: "[X]", Warn: "[!]", Info: "[i]",
		Active: "[..]", Pending: "[ ]", Paused: "[P]",
		Retrying: "[R]", Skipped: "[-]",
		Cursor: ">", Bullet: "*",
		Dash: "-", Ellipsis: "...",
		Arrow: "->", Branch: "|-", Corner: "`-",
		BarFull: "#", BarEmpty: ".",
	}
)

// Sym is the active glyph set.
var Sym = pickGlyphs()

func pickGlyphs() Glyphs {
	if ASCII() {
		return GlyphsASCII
	}
	return GlyphsUnicode
}

// ASCII reports whether to fall back to ASCII glyphs.
//
// IMT_ASCII forces the fallback and IMT_UNICODE forces Unicode; otherwise only
// a legacy Windows console gets ASCII. The overrides exist because the
// detection below is a heuristic and a user on an unusual terminal should not
// have to argue with it.
//
// When both are set, ASCII wins. That is the safe direction: someone who set
// IMT_ASCII most likely has a terminal that renders Unicode as mojibake, and
// honouring the other variable there produces unreadable output, whereas the
// reverse merely produces plainer output than necessary.
func ASCII() bool {
	if os.Getenv("IMT_ASCII") != "" {
		return true
	}
	if os.Getenv("IMT_UNICODE") != "" {
		return false
	}
	if runtime.GOOS != "windows" {
		return false // macOS, Linux and WSL all handle UTF-8
	}
	// Modern Windows terminals set one of these; a bare conhost sets none.
	for _, marker := range []string{"WT_SESSION", "TERM_PROGRAM", "ConEmuANSI"} {
		if os.Getenv(marker) != "" {
			return false
		}
	}
	return os.Getenv("TERM") != ""
}

// SetGlyphs pins a set, for golden tests.
func SetGlyphs(g Glyphs) { Sym = g }
