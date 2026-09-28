package ui

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ichava/maintainer-toolkit/src/internal/core/checker"
)

// capture runs f with output redirected, and returns what went to stderr,
// which is where every status line goes.
func capture(t *testing.T, f func()) string {
	t.Helper()
	var stdout, stderr bytes.Buffer
	Init(Config{NoColor: true, Out: &stdout, Err: &stderr})
	f()
	return stderr.String()
}

// TestSummariseDoesNotClaimSuccessWhenNothingWasChecked is the regression for
// a false green inherited from the Python reporter: it counted stale packs
// only, so a run in which every registry was unreachable found zero stale and
// printed "All packs up to date".
func TestSummariseDoesNotClaimSuccessWhenNothingWasChecked(t *testing.T) {
	allErrored := []checker.Result{
		{Package: "a", Reason: "unreachable: x"},
		{Package: "b", Reason: "unreachable: y"},
	}

	got := capture(t, func() { Summarise(allErrored) })

	if strings.Contains(got, "up to date") {
		t.Fatalf("reported success for a run that checked nothing:\n%s", got)
	}
	if !strings.Contains(got, "no pack could be checked") {
		t.Errorf("summary = %q", got)
	}
	if !strings.Contains(got, "2 of 2") {
		t.Errorf("summary should count the unreachable packs: %q", got)
	}
}

func TestSummariseSeparatesStaleFromUnreachable(t *testing.T) {
	mixed := []checker.Result{
		{Package: "a", Latest: "1.0.0"},                  // ok
		{Package: "b", Latest: "2.0.0", Stale: true},     // behind
		{Package: "c", Reason: "could not parse latest"}, // unknown
	}

	got := capture(t, func() { Summarise(mixed) })

	if !strings.Contains(got, "1 pack(s) behind upstream") {
		t.Errorf("summary = %q", got)
	}
	if !strings.Contains(got, "1 could not be checked") {
		t.Errorf("an unresolved pack must be reported separately: %q", got)
	}
}

func TestSummariseReportsAllClear(t *testing.T) {
	got := capture(t, func() {
		Summarise([]checker.Result{{Package: "a", Latest: "1.0.0"}})
	})
	if !strings.Contains(got, "All packs up to date") {
		t.Errorf("summary = %q", got)
	}
}

func TestSummariseOnAnEmptySetIsNotAFailure(t *testing.T) {
	got := capture(t, func() { Summarise(nil) })
	if strings.Contains(got, "no pack could be checked") {
		t.Errorf("an empty set is vacuously fine, not a failure: %q", got)
	}
}

// TestTableAlignsByDisplayWidthNotByteLength guards the column padding. A
// status cell holds a multi-byte glyph, and padding by len() would misalign
// the row by exactly the number of continuation bytes.
func TestTableAlignsByDisplayWidthNotByteLength(t *testing.T) {
	SetGlyphs(GlyphsUnicode)
	Init(Config{NoColor: true})

	out := Table(
		[]string{"Package", "Status"},
		[][]string{
			{"ichava/icon-sets-flag", "✓ ok"},
			{"ichava/icon-sets-bundled", "✗ error"},
		},
	)

	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want header + 2 rows:\n%s", len(lines), out)
	}
	// Every row's status column must start at the same rune offset.
	want := strings.Index(lines[0], "Status")
	for i, line := range lines[1:] {
		runes := []rune(line)
		idx := -1
		for j := range runes {
			if runes[j] == '✓' || runes[j] == '✗' {
				idx = j
				break
			}
		}
		if idx != want {
			t.Errorf("row %d: status starts at rune %d, header at %d\n%s", i+1, idx, want, out)
		}
	}
}

func TestASCIIFallbackIsForceable(t *testing.T) {
	t.Setenv("IMT_ASCII", "1")
	if !ASCII() {
		t.Error("IMT_ASCII must force the fallback")
	}
	// Both set: ASCII wins, because mojibake is worse than plain text.
	t.Setenv("IMT_UNICODE", "1")
	if !ASCII() {
		t.Error("with both set, IMT_ASCII must win -- it is the safe direction")
	}
}

func TestUnicodeCanBeForcedOnItsOwn(t *testing.T) {
	t.Setenv("IMT_UNICODE", "1")
	if ASCII() {
		t.Error("IMT_UNICODE must force the Unicode set")
	}
}

// TestGlyphSetsCoverTheSameKeys keeps the ASCII fallback from silently
// omitting a symbol, which would render as an empty cell rather than a
// substitute.
func TestGlyphSetsCoverTheSameKeys(t *testing.T) {
	u, a := GlyphsUnicode, GlyphsASCII
	pairs := [][2]string{
		{u.Check, a.Check}, {u.Cross, a.Cross}, {u.Warn, a.Warn}, {u.Info, a.Info},
		{u.Active, a.Active}, {u.Pending, a.Pending}, {u.Paused, a.Paused},
		{u.Retrying, a.Retrying}, {u.Skipped, a.Skipped},
		{u.Cursor, a.Cursor}, {u.Bullet, a.Bullet}, {u.Dash, a.Dash},
		{u.Ellipsis, a.Ellipsis}, {u.Arrow, a.Arrow}, {u.Branch, a.Branch},
		{u.Corner, a.Corner}, {u.BarFull, a.BarFull}, {u.BarEmpty, a.BarEmpty},
	}
	for i, p := range pairs {
		if p[0] == "" || p[1] == "" {
			t.Errorf("glyph pair %d has an empty side: %q / %q", i, p[0], p[1])
		}
	}
}
