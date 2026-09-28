package transforms

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ichava/maintainer-toolkit/src/internal/core/pipeline"
)

// Variant is one upstream directory contributing to a flattened set, and the
// affix that distinguishes its icons from the other variants' once they share
// a directory.
type Variant struct {
	// Subdir is the directory inside the fetched tree.
	Subdir string

	// Prefix and Suffix are applied to the file stem. A variant carrying
	// neither is the unmarked one -- heroicons marks all four, while
	// teenyicons leaves `solid` bare and suffixes only `outline`.
	Prefix string
	Suffix string
}

// FlattenVariants merges several upstream directories into one flat tree,
// distinguishing them by an affix on each filename.
//
// Some packs vendor a multi-directory upstream as a single flat set, and a
// manifest path cannot express that: SubsetTo picks one directory, so pointing
// it at heroicons' `24/solid` would refresh a quarter of the set and the sink
// would delete the other three quarters. Measured, those sets are not
// approximations of one variant but exact merges of all of them --
//
//	heroicons      1288 = 324 o- + 324 s- + 324 m- + 316 c-, a perfect round trip
//	teeny-icons    1200 = 600 bare + 600 -o, likewise
//	gmdi          10610 of 10751 committed, the remainder retired upstream
//
// -- so the rule is recoverable exactly rather than being a guess about what
// the pack maintainer once did by hand.
type FlattenVariants struct {
	pipeline.TransformKind

	// Variants are the directories to merge. Order is irrelevant to the
	// result and the output is written deterministically regardless.
	Variants []Variant

	// Separator normalises the character upstream puts between words, when
	// the pack spells them differently. `_` and a space are rewritten to it.
	//
	// google-material-design-icons needs this and the other two do not:
	// upstream ships `18_up_rating`, the pack committed `18-up-rating`, and
	// without the rewrite only 607 of 2168 names in each variant line up.
	Separator string
}

func (FlattenVariants) Name() string { return "FlattenVariants" }

func (t FlattenVariants) Execute(ctx *pipeline.Context) error {
	if len(t.Variants) == 0 {
		return nil
	}

	root, ok := ctx.FetchedPath()
	if !ok {
		return fmt.Errorf("FlattenVariants: fetched_path not set; needs a source upstream")
	}

	out, err := os.MkdirTemp(ctx.WorkingDir, "flattened-")
	if err != nil {
		return err
	}

	// Built before anything is written, so a collision is reported with both
	// sources named rather than discovered as a file that is already there.
	plan, err := t.plan(root)
	if err != nil {
		return err
	}

	names := make([]string, 0, len(plan))
	for name := range plan {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		if err := copyInto(plan[name].abs, filepath.Join(out, name)); err != nil {
			return err
		}
	}

	ctx.Metric("flatten_variants", map[string]any{
		"variants": len(t.Variants), "icons": len(plan), "target": out,
	})
	ctx.SetFetchedPath(out)
	return nil
}

// origin records where a flattened icon came from, so a collision can name
// both sides.
type origin struct {
	abs string
	rel string
}

// plan resolves every variant to its output name and refuses a collision.
//
// Two variants can produce the same filename when an upstream icon's own name
// already ends in another variant's suffix -- `outline/foo.svg` and
// `solid/foo-o.svg` both flatten to `foo-o.svg`. Whichever is copied second
// would win, and the loser would vanish from the pack with the file count
// unchanged, so the retention guard would not see it either. It has not
// happened in this pack (measured: zero collisions across all three sets), and
// that is a fact about today's upstreams rather than a property of the scheme.
func (t FlattenVariants) plan(root string) (map[string]origin, error) {
	plan := map[string]origin{}

	for _, v := range t.Variants {
		subdir := strings.Trim(v.Subdir, "/")
		if subdir == "" {
			return nil, fmt.Errorf("FlattenVariants: a variant has no subdir")
		}
		dir := filepath.Join(root, filepath.FromSlash(subdir))
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			return nil, fmt.Errorf("FlattenVariants: variant %q not found under %s", v.Subdir, root)
		}

		found := 0
		err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.EqualFold(filepath.Ext(path), ".svg") {
				return nil
			}

			name := v.Prefix + t.normalise(strings.TrimSuffix(d.Name(), filepath.Ext(d.Name()))) + v.Suffix + ".svg"
			rel := subdir + "/" + d.Name()

			if prev, clash := plan[name]; clash {
				return fmt.Errorf(
					"FlattenVariants: %q would be written twice, from %s and %s -- "+
						"one variant's suffix collides with an icon's own name, and the second "+
						"copy would silently replace the first at an unchanged file count",
					name, prev.rel, rel)
			}
			plan[name] = origin{abs: path, rel: rel}
			found++
			return nil
		})
		if err != nil {
			return nil, err
		}
		if found == 0 {
			return nil, fmt.Errorf("FlattenVariants: variant %q holds no SVGs", v.Subdir)
		}
	}
	return plan, nil
}

// normalise rewrites word separators when the pack and its upstream disagree.
func (t FlattenVariants) normalise(stem string) string {
	if t.Separator == "" {
		return stem
	}
	return strings.NewReplacer("_", t.Separator, " ", t.Separator).Replace(stem)
}

func copyInto(src, dest string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	info, err := in.Stat()
	if err != nil {
		return err
	}

	out, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
