package svg

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// The corpus differential.
//
// The SVG layer is the riskiest part of this port: lxml is a real DOM and Go's
// encoding/xml is a streaming decoder, so the two had to be made to agree
// rather than assumed to. A fixture suite cannot establish that -- it would
// have passed just as happily with `style` missing from the allow-list, which
// is the defect this policy's own comments warn about and which silently
// breaks 261 of metronic's 501 icons.
//
// So this runs both implementations over every real vendored icon in whichever
// sibling packs are checked out, and requires them to agree on two things per
// file: the exact list of what the policy removed, and the structure of what
// survived. It deliberately does not compare bytes -- attribute quoting,
// self-closing style and whitespace differ between any two serialisers and no
// consumer can observe them, so a byte diff would fail loudly on things that
// do not matter while telling us nothing about the things that do.

type corpusRecord struct {
	Removed   []string `json:"removed"`
	Inventory []string `json:"inventory"`
	Error     string   `json:"error"`
}

// pythonHarness loads svg_filter directly, bypassing the package __init__,
// which imports rich and is not installed everywhere.
const pythonHarness = `
import importlib.util, json, sys, types
from pathlib import Path

src = Path(sys.argv[1])
# Stub the parent packages so the submodule import resolves without running
# the real __init__, which pulls in rich.
for name in ("ichava_maintainer_toolkit", "ichava_maintainer_toolkit.core",
             "ichava_maintainer_toolkit.core.transforms"):
    mod = types.ModuleType(name)
    mod.__path__ = [str(src / name.replace(".", "/"))]
    sys.modules[name] = mod

def load(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    mod = importlib.util.module_from_spec(spec)
    sys.modules[name] = mod
    spec.loader.exec_module(mod)
    return mod

base = src / "ichava_maintainer_toolkit/core/transforms"
load("ichava_maintainer_toolkit.core.transforms.svg_policy", base / "svg_policy.py")
sf = load("ichava_maintainer_toolkit.core.transforms.svg_filter", base / "svg_filter.py")
from lxml import etree

def inventory(el, path, out):
    here = path + "/" + sf.local_name(el.tag)
    out.append("e " + here)
    for key in el.attrib:
        out.append("a " + here + "@" + sf.attribute_name(key))
    for child in el:
        if isinstance(child.tag, str):
            inventory(child, here, out)

results = {}
for line in sys.stdin.read().splitlines():
    if not line:
        continue
    try:
        raw = Path(line).read_bytes()
        out, removed = sf.sanitise_bytes(raw)
        inv = []
        inventory(etree.fromstring(out, parser=sf._parser()), "", inv)
        results[line] = {"removed": sorted(set(removed)), "inventory": sorted(inv), "error": ""}
    except Exception as e:
        results[line] = {"removed": [], "inventory": [], "error": type(e).__name__}

json.dump(results, sys.stdout)
`

// packsRoot locates the sibling pack checkouts. The toolkit sits beside them.
func packsRoot(t *testing.T) string {
	t.Helper()
	// From src/internal/core/svg -> up five is the directory holding the
	// toolkit checkout and its sibling packs.
	root, err := filepath.Abs(filepath.Join("..", "..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func collectSVGs(t *testing.T, root string, limit int) []string {
	t.Helper()
	var files []string
	for _, pack := range []string{
		"icon-sets-flag", "icon-sets-metronic", "icon-sets-tabler", "icon-sets-emoji",
	} {
		dir := filepath.Join(root, pack, "resources", "assets", "svg")
		if _, err := os.Stat(dir); err != nil {
			continue
		}
		_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".svg") {
				return nil
			}
			files = append(files, path)
			return nil
		})
	}
	sort.Strings(files)

	// Sample evenly rather than truncating, so the subset spans every pack and
	// every category directory instead of stopping inside the first one.
	if limit > 0 && len(files) > limit {
		step := len(files) / limit
		sampled := make([]string, 0, limit)
		for i := 0; i < len(files); i += step {
			sampled = append(sampled, files[i])
		}
		files = sampled
	}
	return files
}

// TestCorpusAgreesWithLxml is the gate. Set IMT_CORPUS_FULL=1 for all of them.
func TestCorpusAgreesWithLxml(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not on PATH")
	}
	if out, err := exec.Command(python, "-c", "import lxml.etree").CombinedOutput(); err != nil {
		t.Skipf("lxml not importable, so there is nothing to compare against: %s", out)
	}

	root := packsRoot(t)
	limit := 1200
	if os.Getenv("IMT_CORPUS_FULL") != "" {
		limit = 0
	}

	files := collectSVGs(t, root, limit)
	if len(files) == 0 {
		t.Skipf("no sibling pack checkouts under %s", root)
	}

	srcDir, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(python, "-c", pythonHarness, srcDir)
	cmd.Stdin = strings.NewReader(strings.Join(files, "\n"))
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			t.Fatalf("python harness failed: %s", ee.Stderr)
		}
		t.Fatal(err)
	}

	var want map[string]corpusRecord
	if err := json.Unmarshal(out, &want); err != nil {
		t.Fatalf("decoding python output: %v", err)
	}

	var mismatches, compared, bothRejected int
	for _, path := range files {
		expected, ok := want[path]
		if !ok {
			t.Errorf("%s: python produced no record", path)
			continue
		}

		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		serialised, removed, gerr := SanitiseBytes(raw, false)

		// Both refusing to parse is agreement, and the interesting kind: this
		// gate runs before a file is committed, so refusing malformed input is
		// the documented behaviour of both implementations.
		if expected.Error != "" {
			if gerr == nil {
				mismatches++
				t.Errorf("%s: python refused (%s) but Go accepted it", rel(root, path), expected.Error)
			} else {
				bothRejected++
			}
			continue
		}
		if gerr != nil {
			mismatches++
			t.Errorf("%s: Go refused (%v) but python accepted it", rel(root, path), gerr)
			continue
		}

		compared++

		if got := dedupeSorted(removed); !equal(got, expected.Removed) {
			mismatches++
			t.Errorf("%s: removed differs\n  go:     %v\n  python: %v", rel(root, path), got, expected.Removed)
			if mismatches > 10 {
				t.Fatal("too many mismatches; stopping")
			}
			continue
		}

		reparsed, err := Parse(serialised, MustLoad().StripComments)
		if err != nil {
			t.Errorf("%s: Go output does not re-parse: %v", rel(root, path), err)
			continue
		}
		if got := Inventory(reparsed); !equal(got, expected.Inventory) {
			mismatches++
			t.Errorf("%s: surviving structure differs\n  go:     %v\n  python: %v",
				rel(root, path), diffFirst(got, expected.Inventory), diffFirst(expected.Inventory, got))
			if mismatches > 10 {
				t.Fatal("too many mismatches; stopping")
			}
		}
	}

	if mismatches == 0 {
		t.Logf("agrees with lxml on %d icons (%d compared, %d rejected by both)",
			len(files), compared, bothRejected)
	}
}

func rel(root, path string) string {
	if r, err := filepath.Rel(root, path); err == nil {
		return r
	}
	return path
}

func dedupeSorted(items []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(items))
	for _, i := range items {
		if !seen[i] {
			seen[i] = true
			out = append(out, i)
		}
	}
	sort.Strings(out)
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// diffFirst returns up to five entries in a that are absent from b, which is
// far more readable than dumping two full inventories.
func diffFirst(a, b []string) []string {
	inB := make(map[string]bool, len(b))
	for _, x := range b {
		inB[x] = true
	}
	var only []string
	for _, x := range a {
		if !inB[x] {
			only = append(only, x)
			if len(only) == 5 {
				break
			}
		}
	}
	return only
}
