package bundled

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeSVGs(t *testing.T, dir string, files map[string]string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestFingerprintSetKeysOnTheLeafName(t *testing.T) {
	root := t.TempDir()
	// A pack set is flat, but an upstream commonly nests by variant. Keying on
	// the leaf is what lets a nested upstream directory be recognised.
	writeSVGs(t, filepath.Join(root, "nested"), map[string]string{"home.svg": "<svg/>"})
	writeSVGs(t, root, map[string]string{"user.svg": "<svg/>"})

	fp, err := FingerprintSet(root)
	if err != nil {
		t.Fatal(err)
	}
	if fp.Count() != 2 {
		t.Fatalf("count = %d, want 2", fp.Count())
	}
	if _, ok := fp.Hashes["home"]; !ok {
		t.Error("a nested icon was not keyed by its leaf name")
	}
}

func TestScoreDirectoryCountsIdenticalSeparatelyFromPresent(t *testing.T) {
	set := writeSVGs(t, filepath.Join(t.TempDir(), "set"), map[string]string{
		"home.svg": "<svg>A</svg>",
		"user.svg": "<svg>B</svg>",
		"gear.svg": "<svg>C</svg>",
	})
	fp, _ := FingerprintSet(set)

	upstream := writeSVGs(t, filepath.Join(t.TempDir(), "up"), map[string]string{
		"home.svg":  "<svg>A</svg>",  // identical
		"user.svg":  "<svg>B!</svg>", // present, different bytes
		"other.svg": "<svg>Z</svg>",  // extra
	})

	c, err := ScoreDirectory(upstream, fp)
	if err != nil {
		t.Fatal(err)
	}
	if c.Present != 2 || c.Identical != 1 || c.Extra != 1 {
		t.Errorf("present=%d identical=%d extra=%d, want 2/1/1", c.Present, c.Identical, c.Extra)
	}
}

// TestClassifySeparatesUpstreamFromVersion is the distinction the audit of the
// real pack forced. ionicons matched 1356 of 1356 names with zero identical
// bytes -- an older release of the right project, not a wrong project.
func TestClassifySeparatesUpstreamFromVersion(t *testing.T) {
	for _, tc := range []struct {
		name    string
		c       Candidate
		setSize int
		want    Confidence
	}{
		{"all bytes", Candidate{Present: 100, Identical: 100}, 100, ConfidenceExact},
		{"most bytes", Candidate{Present: 100, Identical: 85}, 100, ConfidenceStrong},
		{"all names, no bytes", Candidate{Present: 100, Identical: 0}, 100, ConfidencePackage},
		{"most names, no bytes", Candidate{Present: 96, Identical: 0}, 100, ConfidencePackage},
		{"some names", Candidate{Present: 40, Identical: 0}, 100, ConfidencePartial},
		{"nothing", Candidate{}, 100, ConfidenceNone},
		{"empty set", Candidate{Present: 5}, 0, ConfidenceNone},
	} {
		if got := Classify(tc.c, tc.setSize); got != tc.want {
			t.Errorf("%s: Classify = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// TestScorePrefersBytesOverNames is the guard against accepting a different
// project. Two icon sets from the same design language share a great many
// names, so name overlap alone must never outrank byte identity.
func TestScorePrefersBytesOverNames(t *testing.T) {
	manyNames := Candidate{Present: 500, Identical: 0}
	fewBytes := Candidate{Present: 10, Identical: 10}

	if fewBytes.Score() <= manyNames.Score() {
		t.Error("500 name matches outranked 10 byte-identical files")
	}
}

func TestBestDirectoryFindsTheNestedOne(t *testing.T) {
	set := writeSVGs(t, filepath.Join(t.TempDir(), "set"), map[string]string{
		"home.svg": "<svg>A</svg>",
		"user.svg": "<svg>B</svg>",
	})
	fp, _ := FingerprintSet(set)

	root := t.TempDir()
	writeSVGs(t, filepath.Join(root, "docs"), map[string]string{"logo.svg": "<svg>X</svg>"})
	writeSVGs(t, filepath.Join(root, "dist", "icons"), map[string]string{
		"home.svg": "<svg>A</svg>",
		"user.svg": "<svg>B</svg>",
	})

	best, err := BestDirectory(root, fp)
	if err != nil {
		t.Fatal(err)
	}
	if best.Dir != filepath.Join("dist", "icons") {
		t.Errorf("dir = %q, want dist/icons", best.Dir)
	}
	if best.Identical != 2 {
		t.Errorf("identical = %d, want 2", best.Identical)
	}
}

func TestCandidatePackagesCoversTheShapesTheseUpstreamsUse(t *testing.T) {
	for _, tc := range []struct{ set, want string }{
		{"bootstrap-icons", "bootstrap-icons"}, // the bare name, 46 of 72 sets
		{"lucide-icons", "lucide"},             // trimmed
		{"prime-icons", "primeicons"},          // de-hyphenated
		{"mdi", "@mdi/svg"},                    // scoped, names the format
		{"phosphor-icons", "@phosphor-icons/core"},
		{"lucide-icons", "lucide-static"},
		{"fluentui-system-icons", "@fluentui/svg-icons"},
	} {
		got := CandidatePackages(tc.set)
		if !contains(got, tc.want) {
			t.Errorf("CandidatePackages(%q) missing %q; got %v", tc.set, tc.want, got)
		}
	}
}

func TestCandidatePackagesDoesNotRepeat(t *testing.T) {
	got := CandidatePackages("mdi")
	seen := map[string]bool{}
	for _, c := range got {
		if seen[c] {
			t.Errorf("duplicate candidate %q in %v", c, got)
		}
		seen[c] = true
	}
}

// stubFetcher serves prepared package trees without a network.
type stubFetcher struct {
	trees map[string]map[string]string // package -> relative path -> body
}

func (s stubFetcher) Fetch(_ context.Context, pkg, dest string) (string, error) {
	tree, ok := s.trees[pkg]
	if !ok {
		return "", os.ErrNotExist
	}
	for rel, body := range tree {
		full := filepath.Join(dest, "x", rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			return "", err
		}
	}
	return "1.2.3", nil
}

func TestResolveSetStopsAtAnExactMatch(t *testing.T) {
	set := writeSVGs(t, filepath.Join(t.TempDir(), "demo-icons"), map[string]string{
		"home.svg": "<svg>A</svg>",
		"user.svg": "<svg>B</svg>",
	})

	fetch := stubFetcher{trees: map[string]map[string]string{
		"demo-icons": {
			"package/icons/home.svg": "<svg>A</svg>",
			"package/icons/user.svg": "<svg>B</svg>",
		},
	}}

	got := ResolveSet(context.Background(), set, fetch, t.TempDir(), nil)

	if got.Confidence != ConfidenceExact {
		t.Fatalf("confidence = %q, note %q", got.Confidence, got.Note)
	}
	// The path is relative to the npm wrapper, not the extraction root: that is
	// the frame NpmTarball hands downstream, so it is the frame SubsetTo reads.
	// Recording "package/icons" here made the recipe fail to find it.
	if got.Package != "demo-icons" || got.Path != "icons" {
		t.Errorf("package = %q path = %q", got.Package, got.Path)
	}
	if got.Identical != 2 {
		t.Errorf("identical = %d", got.Identical)
	}
}

// TestResolveSetAcceptsAnExplicitCandidate is the loop for the sets the name
// patterns cannot reach: a human supplies a package and the tool answers
// whether its bytes are the ones already committed.
func TestResolveSetAcceptsAnExplicitCandidate(t *testing.T) {
	set := writeSVGs(t, filepath.Join(t.TempDir(), "weird-name"), map[string]string{
		"home.svg": "<svg>A</svg>",
	})
	fetch := stubFetcher{trees: map[string]map[string]string{
		"@scope/nothing-like-the-set": {"package/svg/home.svg": "<svg>A</svg>"},
	}}

	got := ResolveSet(context.Background(), set, fetch, t.TempDir(), nil, "@scope/nothing-like-the-set")

	if got.Confidence != ConfidenceExact {
		t.Fatalf("confidence = %q, note %q", got.Confidence, got.Note)
	}
	if got.Path != "svg" {
		t.Errorf("path = %q, want it relative to the npm package/ wrapper", got.Path)
	}
	if got.Package != "@scope/nothing-like-the-set" {
		t.Errorf("package = %q", got.Package)
	}
}

func TestResolveSetReportsWhenNothingMatched(t *testing.T) {
	set := writeSVGs(t, filepath.Join(t.TempDir(), "demo"), map[string]string{"home.svg": "<svg>A</svg>"})

	got := ResolveSet(context.Background(), set, stubFetcher{}, t.TempDir(), nil)

	if got.Confidence != ConfidenceNone {
		t.Errorf("confidence = %q", got.Confidence)
	}
	if !strings.Contains(got.Note, "tried") {
		t.Errorf("note = %q, want it to name what was tried", got.Note)
	}
}

func TestResolveSetNotesThatAPackageMatchNeedsAVersion(t *testing.T) {
	set := writeSVGs(t, filepath.Join(t.TempDir(), "demo"), map[string]string{
		"a.svg": "<svg>1</svg>", "b.svg": "<svg>2</svg>",
	})
	fetch := stubFetcher{trees: map[string]map[string]string{
		"demo": {"package/a.svg": "<svg>1 changed</svg>", "package/b.svg": "<svg>2 changed</svg>"},
	}}

	got := ResolveSet(context.Background(), set, fetch, t.TempDir(), nil)

	if got.Confidence != ConfidencePackage {
		t.Fatalf("confidence = %q", got.Confidence)
	}
	if !strings.Contains(got.Note, "pin the version") {
		t.Errorf("note = %q, want it to say the remaining work is a version", got.Note)
	}
}

// TestListSetsSkipsDottedEntries: an untracked .claude directory is currently
// sitting in the pack's files/ tree, and counting it made an earlier survey
// report 73 sets where the pack ships 72.
func TestListSetsSkipsDottedEntries(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"mdi", "heroicons", ".claude", ".DS_Store"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	sets, err := ListSets(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(sets) != 2 || sets[0] != "heroicons" || sets[1] != "mdi" {
		t.Errorf("sets = %v, want [heroicons mdi] sorted", sets)
	}
}

func TestSortResolutionsPutsTheWorkFirst(t *testing.T) {
	rs := []Resolution{
		{Set: "a", Confidence: ConfidenceExact},
		{Set: "b", Confidence: ConfidenceNone},
		{Set: "c", Confidence: ConfidencePackage},
		{Set: "d", Confidence: ConfidencePartial},
	}
	SortResolutions(rs)

	want := []string{"b", "d", "c", "a"}
	for i, r := range rs {
		if r.Set != want[i] {
			t.Fatalf("order = %v, want %v", setNames(rs), want)
		}
	}
}

func TestVersionFromTarball(t *testing.T) {
	for _, tc := range []struct{ tarball, pkg, want string }{
		{"bootstrap-icons-1.13.1.tgz", "bootstrap-icons", "1.13.1"},
		{"mdi-svg-7.4.47.tgz", "@mdi/svg", "7.4.47"},
		{"lucide-static-1.47.0.tgz", "lucide-static", "1.47.0"},
		{"phosphor-icons-core-2.1.1.tgz", "@phosphor-icons/core", "2.1.1"},
	} {
		if got := versionFromTarball(tc.tarball, tc.pkg); got != tc.want {
			t.Errorf("versionFromTarball(%q, %q) = %q, want %q", tc.tarball, tc.pkg, got, tc.want)
		}
	}
}

func contains(items []string, want string) bool {
	for _, i := range items {
		if i == want {
			return true
		}
	}
	return false
}

func setNames(rs []Resolution) []string {
	var out []string
	for _, r := range rs {
		out = append(out, r.Set)
	}
	return out
}
