package transforms

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ichava/maintainer-toolkit/src/internal/core/pipeline"
)

// tree builds a fetched tree and a context pointing at it.
func tree(t *testing.T, files map[string]string) (*pipeline.Context, string) {
	t.Helper()
	root := t.TempDir()
	for name, body := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ctx := &pipeline.Context{
		PipelineName: "test", WorkingDir: root,
		Config: map[string]any{}, Extras: map[string]any{}, Metrics: map[string]any{},
	}
	ctx.SetFetchedPath(root)
	return ctx, root
}

func TestSubsetToNarrowsTheTree(t *testing.T) {
	ctx, root := tree(t, map[string]string{
		"icons/home.svg": "<svg/>",
		"package.json":   "{}",
	})

	if err := (SubsetTo{Subdir: "icons"}).Execute(ctx); err != nil {
		t.Fatal(err)
	}
	got, _ := ctx.FetchedPath()
	if got != filepath.Join(root, "icons") {
		t.Errorf("fetched_path = %s", got)
	}
}

func TestSubsetToOnAMissingSubdirIsAnError(t *testing.T) {
	ctx, _ := tree(t, map[string]string{"a.svg": "<svg/>"})
	if err := (SubsetTo{Subdir: "nope"}).Execute(ctx); err == nil {
		t.Fatal("expected an error: a missing source_path means the config is wrong, " +
			"and silently syncing the whole tarball would be worse")
	}
}

func TestSubsetToWithNoSubdirIsANoOp(t *testing.T) {
	ctx, root := tree(t, map[string]string{"a.svg": "<svg/>"})
	if err := (SubsetTo{}).Execute(ctx); err != nil {
		t.Fatal(err)
	}
	if got, _ := ctx.FetchedPath(); got != root {
		t.Errorf("fetched_path = %s, want it unchanged", got)
	}
}

func TestSanitiseRewritesOnlyWhatChanged(t *testing.T) {
	ctx, root := tree(t, map[string]string{
		"clean.svg": `<svg xmlns="http://www.w3.org/2000/svg"><path d="M0 0"/></svg>`,
		"dirty.svg": `<svg xmlns="http://www.w3.org/2000/svg"><path d="M0 0" onload="x()"/></svg>`,
	})

	cleanBefore, _ := os.ReadFile(filepath.Join(root, "clean.svg"))

	if err := (Sanitise{}).Execute(ctx); err != nil {
		t.Fatal(err)
	}

	cleanAfter, _ := os.ReadFile(filepath.Join(root, "clean.svg"))
	if string(cleanBefore) != string(cleanAfter) {
		t.Errorf("an already-clean file was rewritten:\n%s\n%s", cleanBefore, cleanAfter)
	}

	dirty, _ := os.ReadFile(filepath.Join(root, "dirty.svg"))
	if strings.Contains(string(dirty), "onload") {
		t.Errorf("the handler survived: %s", dirty)
	}

	metrics, _ := ctx.Metrics["sanitise"].(map[string]any)
	if metrics["scanned"] != 2 || metrics["cleaned"] != 1 || metrics["violations"] != 1 {
		t.Errorf("metrics = %v", metrics)
	}
}

// TestSanitiseLeavesAnUnparseableFileAlone matters because the alternative is
// worse either way: dropping it silently shrinks the pack, and rewriting a
// guess at it corrupts an icon nobody asked us to fix.
func TestSanitiseLeavesAnUnparseableFileAlone(t *testing.T) {
	broken := `<svg xmlns="http://www.w3.org/2000/svg"><path d=M0 0/></svg>`
	ctx, root := tree(t, map[string]string{"broken.svg": broken})

	if err := (Sanitise{}).Execute(ctx); err != nil {
		t.Fatalf("one unparseable file must not fail the whole transform: %v", err)
	}

	after, _ := os.ReadFile(filepath.Join(root, "broken.svg"))
	if string(after) != broken {
		t.Errorf("the file was modified:\n%s", after)
	}
	metrics, _ := ctx.Metrics["sanitise"].(map[string]any)
	if metrics["unparsable"] != 1 {
		t.Errorf("metrics = %v, want unparsable=1", metrics)
	}
}

func TestSanitiseStrictFailsOnAViolation(t *testing.T) {
	ctx, _ := tree(t, map[string]string{
		"a.svg": `<svg xmlns="http://www.w3.org/2000/svg"><script>x</script></svg>`,
	})
	if err := (Sanitise{Strict: true}).Execute(ctx); err == nil {
		t.Fatal("strict mode must fail on a violation")
	}
}

func TestSlugify(t *testing.T) {
	ctx, root := tree(t, map[string]string{
		"Arrow Up.svg":    "<svg/>",
		"already-ok.svg":  "<svg/>",
		"WEIRD__name.SVG": "<svg/>",
	})

	if err := (Slugify{}).Execute(ctx); err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{"arrow-up.svg", "already-ok.svg", "weird-name.svg"} {
		if _, err := os.Stat(filepath.Join(root, want)); err != nil {
			entries, _ := os.ReadDir(root)
			var got []string
			for _, e := range entries {
				got = append(got, e.Name())
			}
			t.Errorf("%s missing; tree is %v", want, got)
		}
	}
}

const cldrSample = `# group: Smileys & Emotion

# subgroup: face-smiling
1F600                                      ; fully-qualified     # 😀 E1.0 grinning face
1F603                                      ; fully-qualified     # 😃 E0.6 grinning face with big eyes
263A FE0F                                  ; fully-qualified     # ☺️ E0.6 smiling face
263A                                       ; unqualified         # ☺ E0.6 smiling face

# group: Animals & Nature

# subgroup: animal-mammal
1F436                                      ; fully-qualified     # 🐶 E0.6 dog face
`

func TestParseCLDR(t *testing.T) {
	records := ParseCLDR(cldrSample)

	// Four fully-qualified rows; the unqualified one is skipped because it is
	// the same emoji with fewer selectors and would collide on output name.
	if len(records) != 4 {
		t.Fatalf("got %d records, want 4: %+v", len(records), records)
	}

	if records[0].Key() != "1f600" || records[0].Group != "Smileys & Emotion" {
		t.Errorf("first record = %+v", records[0])
	}
	// A multi-codepoint sequence joins with a hyphen, matching the filenames
	// upstream ships.
	if records[2].Key() != "263a-fe0f" {
		t.Errorf("multi-codepoint key = %q", records[2].Key())
	}
	// The group heading carries forward to the rows beneath it.
	if records[3].Group != "Animals & Nature" {
		t.Errorf("last record group = %q", records[3].Group)
	}
}

func TestSlugifyGroupAndName(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"Smileys & Emotion", "smileys-emotion"},
		{"Animals & Nature", "animals-nature"},
		{"Travel & Places", "travel-places"},
	} {
		if got := SlugifyGroup(tc.in); got != tc.want {
			t.Errorf("SlugifyGroup(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	for _, tc := range []struct{ in, want string }{
		{"grinning face", "grinning-face"},
		{"flag: Japan", "flag-japan"},
		{"keycap: *", "keycap"},
	} {
		if got := SlugifyName(tc.in); got != tc.want {
			t.Errorf("SlugifyName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestCategoriseFilesByGroupAndRenamesToTheSlug(t *testing.T) {
	ctx, root := tree(t, map[string]string{
		"1F600.svg":     "<svg/>",
		"1F436.svg":     "<svg/>",
		"263A-FE0F.svg": "<svg/>",
		"DEADBEEF.svg":  "<svg/>", // not in the taxonomy
	})
	ctx.SetString(pipeline.KeyCLDRText, cldrSample)

	if err := (Categorise{By: "cldr"}).Execute(ctx); err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{
		"smileys-emotion/grinning-face.svg",
		"smileys-emotion/smiling-face.svg",
		"animals-nature/dog-face.svg",
	} {
		if _, err := os.Stat(filepath.Join(root, want)); err != nil {
			t.Errorf("%s missing", want)
		}
	}

	metrics, _ := ctx.Metrics["categorise"].(map[string]any)
	if metrics["copied"] != 3 || metrics["missing"] != 1 {
		t.Errorf("metrics = %v, want copied=3 missing=1", metrics)
	}
	if _, err := os.Stat(filepath.Join(root, "1F600.svg")); err == nil {
		t.Error("the flat original survived alongside the categorised copy")
	}
}

// TestCategoriseRefusesAnEmptyTaxonomy: with no records every file lands in
// "missing" and the pack is silently emptied.
func TestCategoriseRefusesAnEmptyTaxonomy(t *testing.T) {
	ctx, _ := tree(t, map[string]string{"1F600.svg": "<svg/>"})
	ctx.SetString(pipeline.KeyCLDRText, "# nothing parseable here\n")

	if err := (Categorise{By: "cldr"}).Execute(ctx); err == nil {
		t.Fatal("an empty taxonomy must be an error, not a silent wipe")
	}
}

func TestCategorisePassthroughIsANoOp(t *testing.T) {
	ctx, root := tree(t, map[string]string{"a.svg": "<svg/>"})
	if err := (Categorise{By: "passthrough"}).Execute(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "a.svg")); err != nil {
		t.Error("passthrough moved a file")
	}
}

func TestCategoriseRejectsAnUnknownTaxonomy(t *testing.T) {
	ctx, _ := tree(t, map[string]string{"a.svg": "<svg/>"})
	if err := (Categorise{By: "astrology"}).Execute(ctx); err == nil {
		t.Fatal("expected an error naming the unknown taxonomy")
	}
}

func TestIndexerWritesBothLookups(t *testing.T) {
	ctx, root := tree(t, map[string]string{"x/a.svg": "<svg/>"})
	ctx.SetFetchedPath(filepath.Join(root, "x"))
	ctx.Extras[pipeline.KeyCLDRRecords] = ParseCLDR(cldrSample)

	if err := (Indexer{Targets: []string{"codepoints", "names"}}).Execute(ctx); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(filepath.Join(root, "codepoints.json"))
	if err != nil {
		t.Fatal(err)
	}
	var codepoints map[string]string
	if err := json.Unmarshal(raw, &codepoints); err != nil {
		t.Fatal(err)
	}
	if codepoints["1F600"] != "smileys-emotion/grinning-face" {
		t.Errorf("codepoints[1F600] = %q", codepoints["1F600"])
	}

	// The ampersand in a CLDR group name must not arrive HTML-escaped: on by
	// default Go would write & and the file would differ from the
	// Python's for no reason anyone could see in a diff.
	if strings.Contains(string(raw), `&`) {
		t.Error("output is HTML-escaped")
	}

	if _, err := os.Stat(filepath.Join(root, "names.json")); err != nil {
		t.Error("names.json missing")
	}
}

func TestIndexerNeedsRecords(t *testing.T) {
	ctx, _ := tree(t, map[string]string{"a.svg": "<svg/>"})
	if err := (Indexer{Targets: []string{"names"}}).Execute(ctx); err == nil {
		t.Fatal("expected an error naming the missing cldr_records")
	}
}
