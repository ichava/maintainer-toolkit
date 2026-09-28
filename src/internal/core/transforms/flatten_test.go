package transforms

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// flattened lists the result of a FlattenVariants run.
func flattened(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names
}

// TestFlattenPrefixesEachVariant is the heroicons shape: four upstream
// directories, every one of them marked, merged into one flat set.
//
// Upstream ships 24/outline, 24/solid, 20/solid and 16/solid; the pack commits
// them flat as o-, s-, m- and c-. Measured against the real packages the merge
// round-trips exactly -- 1288 produced against 1288 committed, nothing new and
// nothing dropped.
func TestFlattenPrefixesEachVariant(t *testing.T) {
	ctx, _ := tree(t, map[string]string{
		"24/outline/bell.svg": "<svg>outline</svg>",
		"24/solid/bell.svg":   "<svg>solid</svg>",
		"20/solid/bell.svg":   "<svg>mini</svg>",
		"16/solid/bell.svg":   "<svg>micro</svg>",
		"package.json":        "{}",
	})

	err := FlattenVariants{Variants: []Variant{
		{Subdir: "24/outline", Prefix: "o-"},
		{Subdir: "24/solid", Prefix: "s-"},
		{Subdir: "20/solid", Prefix: "m-"},
		{Subdir: "16/solid", Prefix: "c-"},
	}}.Execute(ctx)
	if err != nil {
		t.Fatal(err)
	}

	out, _ := ctx.FetchedPath()
	got := flattened(t, out)
	want := []string{"c-bell.svg", "m-bell.svg", "o-bell.svg", "s-bell.svg"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("got %v, want %v", got, want)
	}

	// The affix has to carry the right bytes, not just the right name -- a
	// merge that labelled every file correctly and copied one variant's
	// contents four times would pass a name-only assertion.
	body, err := os.ReadFile(filepath.Join(out, "c-bell.svg"))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "<svg>micro</svg>" {
		t.Errorf("c-bell.svg holds %q, so it came from the wrong variant", body)
	}

	// package.json is outside every variant and must not be carried over.
	if _, err := os.Stat(filepath.Join(out, "package.json")); !os.IsNotExist(err) {
		t.Error("flattening pulled in a file from outside the variants")
	}
}

// TestFlattenLeavesTheUnmarkedVariantBare is the teenyicons shape: solid keeps
// the plain name and only outline is suffixed.
func TestFlattenLeavesTheUnmarkedVariantBare(t *testing.T) {
	ctx, _ := tree(t, map[string]string{
		"solid/alarm.svg":   "<svg>solid</svg>",
		"outline/alarm.svg": "<svg>outline</svg>",
	})

	err := FlattenVariants{Variants: []Variant{
		{Subdir: "solid"},
		{Subdir: "outline", Suffix: "-o"},
	}}.Execute(ctx)
	if err != nil {
		t.Fatal(err)
	}

	out, _ := ctx.FetchedPath()
	got := flattened(t, out)
	if strings.Join(got, " ") != "alarm-o.svg alarm.svg" {
		t.Fatalf("got %v", got)
	}
}

// TestFlattenNormalisesTheSeparator is google-material-design-icons, which is
// the only set here needing it.
//
// Upstream spells `18_up_rating` and the pack committed `18-up-rating`.
// Without the rewrite only 607 of 2168 names in each variant line up, so the
// set reads as a partial match against the right package.
func TestFlattenNormalisesTheSeparator(t *testing.T) {
	ctx, _ := tree(t, map[string]string{
		"filled/18_up_rating.svg":   "<svg>f</svg>",
		"outlined/18_up_rating.svg": "<svg>o</svg>",
	})

	err := FlattenVariants{
		Separator: "-",
		Variants: []Variant{
			{Subdir: "filled"},
			{Subdir: "outlined", Suffix: "-o"},
		},
	}.Execute(ctx)
	if err != nil {
		t.Fatal(err)
	}

	out, _ := ctx.FetchedPath()
	got := flattened(t, out)
	if strings.Join(got, " ") != "18-up-rating-o.svg 18-up-rating.svg" {
		t.Fatalf("got %v", got)
	}
}

// TestFlattenRefusesACollision covers the way this scheme can lose an icon
// without anything noticing.
//
// An upstream icon whose own name ends in another variant's suffix flattens
// onto that variant's output name. Whichever is copied second wins, the loser
// vanishes, and the file count is unchanged -- so the sink's retention guard
// sees a clean swap and the pack silently ships one icon wearing another's
// name. It does not happen in this pack today (measured: zero collisions
// across all three flattened sets), which is a fact about today's upstreams
// rather than a property of the scheme.
func TestFlattenRefusesACollision(t *testing.T) {
	ctx, _ := tree(t, map[string]string{
		"solid/alarm-o.svg": "<svg>an icon whose name ends in the suffix</svg>",
		"outline/alarm.svg": "<svg>outline</svg>",
	})

	err := FlattenVariants{Variants: []Variant{
		{Subdir: "solid"},
		{Subdir: "outline", Suffix: "-o"},
	}}.Execute(ctx)

	if err == nil {
		t.Fatal("two variants produced one filename and the merge accepted it")
	}
	for _, want := range []string{"alarm-o.svg", "solid/alarm-o.svg", "outline/alarm.svg"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not name %q, so a reader cannot find the clash: %v", want, err)
		}
	}
}

// TestFlattenRefusesAMissingVariant stops a renamed upstream directory from
// silently shrinking the set.
//
// The sink wipes the destination before copying, so a variant that resolves to
// nothing is not a smaller refresh -- it is a deletion. Failing here leaves the
// pack untouched.
func TestFlattenRefusesAMissingVariant(t *testing.T) {
	ctx, _ := tree(t, map[string]string{"solid/alarm.svg": "<svg/>"})

	err := FlattenVariants{Variants: []Variant{
		{Subdir: "solid"},
		{Subdir: "outline", Suffix: "-o"},
	}}.Execute(ctx)

	if err == nil || !strings.Contains(err.Error(), "outline") {
		t.Fatalf("a missing variant must fail and name itself, got %v", err)
	}
}

// TestFlattenRefusesAnEmptyVariant covers the directory that exists and holds
// no icons, which a `files:`-style glob would treat as success.
func TestFlattenRefusesAnEmptyVariant(t *testing.T) {
	ctx, root := tree(t, map[string]string{"solid/alarm.svg": "<svg/>"})
	if err := os.MkdirAll(filepath.Join(root, "outline"), 0o755); err != nil {
		t.Fatal(err)
	}

	err := FlattenVariants{Variants: []Variant{
		{Subdir: "solid"},
		{Subdir: "outline", Suffix: "-o"},
	}}.Execute(ctx)

	if err == nil || !strings.Contains(err.Error(), "no SVGs") {
		t.Fatalf("an empty variant must fail, got %v", err)
	}
}
