// Package transforms mutates the in-flight asset tree between a source and a
// sink. Every transform reads and writes Context.FetchedPath.
package transforms

import (
	"bytes"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/ichava/maintainer-toolkit/src/internal/core/pipeline"
	"github.com/ichava/maintainer-toolkit/src/internal/core/svg"
)

// walkSVGs returns every .svg under root, in a stable order.
//
// Sorted because the order reaches the progress output and the metrics, and an
// unordered walk makes two runs over the same tree produce logs that cannot be
// diffed.
func walkSVGs(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(strings.ToLower(path), ".svg") {
			files = append(files, path)
		}
		return nil
	})
	sort.Strings(files)
	return files, err
}

// SubsetTo re-points FetchedPath at a subdirectory of the fetched tree.
//
// This is how a source's `source_path` is honoured: npm ships @tabler/icons
// with the SVGs under icons/, and the rest of the tarball is not wanted.
type SubsetTo struct {
	pipeline.TransformKind
	Subdir string
}

func (SubsetTo) Name() string { return "SubsetTo" }

func (t SubsetTo) Execute(ctx *pipeline.Context) error {
	subdir := strings.Trim(t.Subdir, "/")
	if subdir == "" {
		return nil
	}

	root, ok := ctx.FetchedPath()
	if !ok {
		return fmt.Errorf("SubsetTo: fetched_path not set; needs a source upstream")
	}

	target := filepath.Join(root, subdir)
	info, err := os.Stat(target)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("SubsetTo: subdir %q not found under %s", t.Subdir, root)
	}

	ctx.SetFetchedPath(target)
	return nil
}

// Sanitise applies the SVG policy to every file in the tree, in place.
type Sanitise struct {
	pipeline.TransformKind

	// AlsoStripClass removes the class attribute as well. Off by default.
	AlsoStripClass bool

	// Strict turns policy violations into a failure. Off by default, because
	// an upstream that ships one odd icon should not block the other 6,000 --
	// the violation is reported either way.
	Strict bool

	Log *slog.Logger
}

func (Sanitise) Name() string { return "Sanitise" }

func (t Sanitise) Execute(ctx *pipeline.Context) error {
	root, ok := ctx.FetchedPath()
	if !ok {
		return fmt.Errorf("Sanitise: fetched_path not set; needs a source upstream")
	}

	files, err := walkSVGs(root)
	if err != nil {
		return err
	}

	log := t.Log
	if log == nil {
		log = slog.Default()
	}

	var cleaned, unparsable int
	var violations []string

	for _, path := range files {
		original, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		filtered, removed, err := svg.SanitiseBytes(original, t.AlsoStripClass)
		if err != nil {
			// An unparseable file is counted and left exactly as it was.
			// Dropping it would silently shrink the pack; rewriting a guess at
			// it would be worse.
			unparsable++
			log.Warn("sanitise: cannot parse", "file", path, "error", err)
			continue
		}

		if len(removed) > 0 {
			violations = append(violations,
				filepath.Base(path)+": "+svg.SummariseRemoved(removed))
		}

		if !bytes.Equal(filtered, original) {
			if err := os.WriteFile(path, filtered, 0o644); err != nil {
				return err
			}
			cleaned++
		}
	}

	ctx.Metric("sanitise", map[string]any{
		"scanned": len(files), "cleaned": cleaned,
		"unparsable": unparsable, "violations": len(violations),
	})
	log.Info("sanitise", "scanned", len(files), "cleaned", cleaned,
		"unparsable", unparsable, "violations", len(violations))

	if t.Strict && len(violations) > 0 {
		return &svg.PolicyViolationError{Violations: violations}
	}
	return nil
}

// slugPattern and slugTrim implement the shared filename slug rule.
var (
	slugPattern = regexp.MustCompile(`[^a-z0-9.]+`)
	nameSlug    = regexp.MustCompile(`[^a-z0-9]+`)
)

// Slugify lowercases and hyphenates SVG filenames in place.
//
// Not used by either shipped recipe -- both upstreams already ship slugged
// names -- but kept because a new pack from a less tidy upstream is exactly
// when it is needed, and it is four lines.
type Slugify struct{ pipeline.TransformKind }

func (Slugify) Name() string { return "Slugify" }

func (Slugify) Execute(ctx *pipeline.Context) error {
	root, ok := ctx.FetchedPath()
	if !ok {
		return fmt.Errorf("Slugify: fetched_path not set; needs a source upstream")
	}

	files, err := walkSVGs(root)
	if err != nil {
		return err
	}

	renamed := 0
	for _, path := range files {
		dir, base := filepath.Split(path)
		lowered := strings.ToLower(base)

		slug := strings.Trim(slugPattern.ReplaceAllString(lowered, "-"), "-.")
		if !strings.HasSuffix(lowered, ".svg") {
			slug += ".svg"
		}
		if slug == base {
			continue
		}
		if err := os.Rename(path, filepath.Join(dir, slug)); err != nil {
			return err
		}
		renamed++
	}

	ctx.Metric("slugify", map[string]any{"renamed": renamed})
	return nil
}

// SlugifyName is the shared name-to-slug rule, exported because the CLDR
// categoriser and the indexer both need it and must agree.
func SlugifyName(name string) string {
	return strings.Trim(nameSlug.ReplaceAllString(strings.ToLower(name), "-"), "-")
}

// SlugifyGroup slugs a CLDR group name.
//
// Python has two near-copies of this -- categorise's replaces "&" with a space
// and then spaces with hyphens, indexer's only does the first. They produce
// identical output for every CLDR group because SlugifyName already collapses
// any run of non-alphanumerics to a single hyphen, so the second replace is
// redundant. One function here, rather than two that are equal by luck.
func SlugifyGroup(group string) string {
	return SlugifyName(strings.ReplaceAll(group, "&", " "))
}
