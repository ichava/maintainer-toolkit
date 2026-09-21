package checker

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

// versionPairs is the corpus both this package and PHP are run over.
//
// The first block is what the four shipped packs actually poll for. The rest
// are the shapes that separate PHP's ordering from Python's tuple comparison,
// which is the whole reason this port picked a side.
var versionPairs = [][2]string{
	// Real, from config/*.json and the packs' vendored config.json.
	{"3.46.0", "3.47.0"}, {"3.47.0", "3.47.0"}, {"3.47.0", "3.46.0"},
	{"7.0.0", "7.5.0"}, {"15.0.0", "15.1.0"}, {"2.2.0", "2.2.0"},
	{"2.2.0", "2.3.0"}, {"17.0.0", "17.1.0"},

	// Pre-releases: the case Python gets backwards.
	{"3.46.0", "3.46.0-rc1"}, {"3.46.0-rc1", "3.46.0"},
	{"1.0.0", "1.0.0-alpha"}, {"1.0.0-alpha", "1.0.0-beta"},
	{"1.0.0-beta", "1.0.0-rc1"}, {"1.0.0-rc1", "1.0.0"},
	{"1.0.0-dev", "1.0.0-alpha"}, {"1.0.0", "1.0.0-pl1"},

	// Unseparated pre-release, which canonicalisation has to split.
	{"1.0rc1", "1.0"}, {"1.0rc1", "1.0rc2"}, {"1.0a", "1.0b"},

	// Differing lengths.
	{"1.0", "1.0.1"}, {"1.0.1", "1.0"}, {"1", "1.0"}, {"1.0.0", "1"},

	// Separator equivalence: '-', '_' and '+' all canonicalise to '.'.
	{"1.0-1", "1.0.1"}, {"1.0_1", "1.0.1"}, {"1.0+1", "1.0.1"},

	// Leading zeros, and wide numbers.
	{"1.007", "1.7"}, {"1.10", "1.9"}, {"20240101", "20231231"},

	// Date-stamped tags, which some upstreams use.
	{"2024.01.01", "2024.1.2"}, {"20.10", "20.9"},
}

// TestVersionCompareMatchesPHP is the differential that settles the ordering.
//
// The Python it replaces disagrees with PHP on pre-releases and raises
// TypeError on mixed parts, while claiming in its own docstring to mirror PHP
// exactly. Rather than pick by reading either, this runs the real
// version_compare over the corpus and requires agreement on every pair.
func TestVersionCompareMatchesPHP(t *testing.T) {
	php, err := exec.LookPath("php")
	if err != nil {
		t.Skip("php not on PATH; the PHP differential cannot run here")
	}

	payload, err := json.Marshal(versionPairs)
	if err != nil {
		t.Fatal(err)
	}

	script := `
$pairs = json_decode(file_get_contents('php://stdin'), true);
$out = [];
foreach ($pairs as $p) { $out[] = version_compare($p[0], $p[1]); }
echo json_encode($out);
`
	cmd := exec.Command(php, "-d", "opcache.enable_cli=0", "-d", "xdebug.mode=off", "-r", script)
	cmd.Stdin = strings.NewReader(string(payload))
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("running php: %v", err)
	}

	var want []int
	if err := json.Unmarshal(out, &want); err != nil {
		t.Fatalf("decoding php output %q: %v", out, err)
	}
	if len(want) != len(versionPairs) {
		t.Fatalf("php returned %d results for %d pairs", len(want), len(versionPairs))
	}

	mismatches := 0
	for i, pair := range versionPairs {
		got := VersionCompare(pair[0], pair[1])
		if got != want[i] {
			mismatches++
			t.Errorf("VersionCompare(%q, %q) = %d, php version_compare = %d",
				pair[0], pair[1], got, want[i])
		}
	}
	if mismatches == 0 {
		t.Logf("agrees with php version_compare on all %d pairs", len(versionPairs))
	}
}

// TestIsStaleDisagreesWithPythonOnPreReleases states the behaviour change
// outright, so it is a decision on the record rather than a silent drift.
func TestIsStaleDisagreesWithPythonOnPreReleases(t *testing.T) {
	// Python's tuple comparison makes (3,46,0) < (3,46,0,'rc1') true and would
	// report a stable release as behind its own release candidate. PHP, and
	// therefore this port, does not.
	if IsStale("3.46.0", "3.46.0-rc1") {
		t.Error("a stable release must not be reported as behind its own release candidate")
	}
	// The ordinary direction still works.
	if !IsStale("3.46.0", "3.47.0") {
		t.Error("3.46.0 is behind 3.47.0")
	}
	if IsStale("3.47.0", "3.47.0") {
		t.Error("a pack on the latest version is not stale")
	}
}

func TestIsStaleTreatsAnUnknownCurrentAsStale(t *testing.T) {
	if !IsStale("", "1.0.0") {
		t.Error("a pack with no vendored version has never been synced, so it is stale")
	}
}

func TestTrimVersion(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"v3.47.0", "3.47.0"},
		{"V7.5.0", "7.5.0"},
		{" v1.0.0 ", "1.0.0"},
		{"3.47.0", "3.47.0"},
		{"", ""},
		// The shared character-set trim, preserved deliberately: both PHP's
		// ltrim($tag,'vV ') and Python's lstrip("vV ") do exactly this.
		{"vv1.0", "1.0"},
		{"Version1", "ersion1"},
	} {
		if got := TrimVersion(tc.in); got != tc.want {
			t.Errorf("TrimVersion(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestCanonicalizeVersionSplitsDigitLetterBoundaries(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want []string
	}{
		{"1.0rc1", []string{"1", "0", "rc", "1"}},
		{"1.0-rc-1", []string{"1", "0", "rc", "1"}},
		{"3.47.0", []string{"3", "47", "0"}},
		{"1.0_1", []string{"1", "0", "1"}},
		{"1.0+build2", []string{"1", "0", "build", "2"}},
	} {
		got := canonicalizeVersion(tc.in)
		if strings.Join(got, ".") != strings.Join(tc.want, ".") {
			t.Errorf("canonicalizeVersion(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}
