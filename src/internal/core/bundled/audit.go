// Package bundled identifies, and verifies, where each set in the
// icon-sets-bundled aggregator pack actually came from.
//
// The pack vendors 121,314 icons across 72 sets, and nothing records which
// upstream any of them came from. The pack config declares
// `update_command.type: "iconify"`, but the committed files are not
// Iconify-rendered: measured across the pack there are 26 distinct <svg>
// attribute signatures -- bootstrap-icons keeps `class="bi bi-alarm"`,
// heroicons keeps `data-slot`, fontawesome keeps its licence comment -- where
// anything rendered from @iconify-json/* would have exactly one. Each set is
// its own upstream's native distribution.
//
// So a refresh has to go back to each set's own upstream, and that needs a
// manifest that does not exist. This package builds it, and does so by
// measurement rather than by guessing: a candidate upstream is accepted only
// when the files it ships reproduce what is already committed.
package bundled

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// SetFingerprint is what a committed set looks like: its icon names and the
// content hash of each.
type SetFingerprint struct {
	Name   string
	Hashes map[string]string // icon name (no extension) -> sha256
}

// Count returns how many icons the set ships.
func (f SetFingerprint) Count() int { return len(f.Hashes) }

// FingerprintSet reads a committed set directory.
//
// Keyed by the icon name without its extension and without any subdirectory,
// because an upstream commonly nests by variant (heroicons ships 24/solid/)
// where the pack has flattened. Matching on the leaf name is what lets a
// nested upstream directory be recognised as the source of a flat set.
func FingerprintSet(dir string) (SetFingerprint, error) {
	fp := SetFingerprint{Name: filepath.Base(dir), Hashes: map[string]string{}}

	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(strings.ToLower(path), ".svg") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		fp.Hashes[name] = hex.EncodeToString(sum[:])
		return nil
	})
	return fp, err
}

// Candidate is one directory inside a fetched upstream, scored against a set.
type Candidate struct {
	// Dir is the path inside the extracted package.
	Dir string

	// Present is how many of the set's icons this directory also has, by name.
	Present int

	// Identical is how many of those are byte-for-byte the same.
	Identical int

	// Extra is how many icons the directory has that the set does not.
	Extra int
}

// Score ranks candidates: byte-identical matches dominate, then name overlap.
//
// Name overlap alone is not enough to accept a directory. Two icon sets built
// from the same design language share a great many names -- `home`, `user`,
// `search` -- so a directory can look like a strong match and be a different
// project entirely. Only a byte-identical file proves the file came from here.
func (c Candidate) Score() float64 {
	if c.Present == 0 {
		return 0
	}
	return float64(c.Identical)*1000 + float64(c.Present)
}

// Confidence classifies a candidate against the set it was scored on.
type Confidence string

const (
	// ConfidenceExact means every committed icon was reproduced byte for byte.
	// The upstream and the path are then established fact, not inference.
	ConfidenceExact Confidence = "exact"

	// ConfidenceStrong means most icons matched byte for byte. Usually the
	// upstream is right and its version has moved on since the pack vendored
	// it, so some icons were redrawn.
	ConfidenceStrong Confidence = "strong"

	// ConfidencePackage means nearly every icon is present by name but the
	// bytes differ -- the upstream is identified, the version is not.
	//
	// This tier exists because of what the audit found. ionicons matched
	// 1356 of 1356 names with zero byte-identical files: the committed icons
	// expand what upstream now expresses as CSS classes into explicit
	// fill/stroke attributes, and write path data with comma separators. That
	// is an older release of the same project, not a different project. Grading
	// it "partial" alongside a genuine near-miss would bury the distinction a
	// human most needs: here the remaining work is pinning a version, not
	// identifying a source.
	ConfidencePackage Confidence = "package"

	// ConfidencePartial means some names line up but coverage is incomplete.
	// That is the dangerous middle: two icon sets from the same design
	// language share a great many names, so it may be a different project.
	ConfidencePartial Confidence = "partial"

	// ConfidenceNone means nothing lined up.
	ConfidenceNone Confidence = "none"
)

// Classify grades a candidate against the set size.
//
// Two independent signals, deliberately kept apart. Byte identity establishes
// the version as well as the source; name coverage establishes only the
// source. Collapsing them loses the difference between "found it, needs a
// version" and "might be the wrong project entirely".
func Classify(c Candidate, setSize int) Confidence {
	if setSize == 0 || c.Present == 0 {
		return ConfidenceNone
	}
	identical := float64(c.Identical) / float64(setSize)
	present := float64(c.Present) / float64(setSize)

	switch {
	case c.Identical == setSize:
		return ConfidenceExact
	case identical >= 0.80:
		return ConfidenceStrong
	case present >= 0.95:
		return ConfidencePackage
	default:
		return ConfidencePartial
	}
}

// ScoreDirectory compares one directory inside an extracted package against a
// committed set.
func ScoreDirectory(dir string, fp SetFingerprint) (Candidate, error) {
	c := Candidate{Dir: dir}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return c, err
	}

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".svg") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))

		want, known := fp.Hashes[name]
		if !known {
			c.Extra++
			continue
		}
		c.Present++

		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) == want {
			c.Identical++
		}
	}
	return c, nil
}

// BestDirectory walks an extracted package and returns the directory that best
// reproduces the set.
//
// Every directory holding SVGs is scored, not just the likely ones: upstreams
// put them in `icons/`, `dist/icons/`, `svg/`, `src/svg/`, `24/solid/` and
// several other places, and hardcoding that list is how a real source gets
// reported as unresolved.
func BestDirectory(root string, fp SetFingerprint) (Candidate, error) {
	var best Candidate

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil //nolint:nilerr // an unreadable subtree is skipped, not fatal
		}
		c, err := ScoreDirectory(path, fp)
		if err != nil {
			return nil //nolint:nilerr
		}
		if c.Score() > best.Score() {
			best = c
		}
		return nil
	})

	if best.Dir != "" {
		if rel, err := filepath.Rel(root, best.Dir); err == nil {
			best.Dir = rel
		}
	}
	return best, err
}

// Resolution is one set's answer.
type Resolution struct {
	Set        string     `json:"set"`
	Icons      int        `json:"icons"`
	Package    string     `json:"package,omitempty"`
	Version    string     `json:"version,omitempty"`
	Path       string     `json:"path,omitempty"`
	Matched    int        `json:"matched"`
	Identical  int        `json:"identical"`
	Confidence Confidence `json:"confidence"`
	Note       string     `json:"note,omitempty"`
}

// CandidatePackages returns npm package names to try for a set directory.
//
// These are guesses, and they are allowed to be: nothing is accepted on the
// strength of a name. The scoring decides, and a candidate that ships no
// matching bytes is rejected however plausible it looked.
//
// Measured against the pack: the bare directory name resolves as an npm
// package for 46 of the 72 sets, which is why it is tried first.
func CandidatePackages(set string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(name string) {
		if name != "" && !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}

	add(set)
	// `lucide-icons` -> `lucide`, `teeny-icons` -> `teeny`
	trimmed := strings.TrimSuffix(set, "-icons")
	add(trimmed)
	// `prime-icons` -> `primeicons`, `teeny-icons` -> `teenyicons`
	add(strings.ReplaceAll(set, "-", ""))
	add(trimmed + "icons")

	// Scoped packages. The large sets almost all publish this way, and they
	// are exactly the ones an unscoped guess misses: measured over this pack,
	// the six biggest unresolved sets were 52,789 icons between them and every
	// one of them publishes under a scope.
	add("@" + set + "/svg")  // mdi -> @mdi/svg
	add("@" + set + "/core") // phosphor-icons -> @phosphor-icons/core
	add("@" + trimmed + "/svg")
	add("@" + trimmed + "/" + set) // codicons -> @vscode/codicons shape
	add(trimmed + "-static")       // lucide-icons -> lucide-static
	add(set + "-static")

	// `fluentui-system-icons` -> `@fluentui/svg-icons`: the scope is the first
	// dash-separated word and the package names the format.
	if first, _, ok := strings.Cut(set, "-"); ok {
		add("@" + first + "/svg-icons")
		add("@" + first + "/svg")
	}
	return out
}

// SortResolutions orders a report: the ones needing a human first, because
// that is the list somebody has to act on.
func SortResolutions(rs []Resolution) {
	rank := map[Confidence]int{
		ConfidenceNone: 0, ConfidencePartial: 1, ConfidencePackage: 2,
		ConfidenceStrong: 3, ConfidenceExact: 4,
	}
	sort.SliceStable(rs, func(i, j int) bool {
		if rank[rs[i].Confidence] != rank[rs[j].Confidence] {
			return rank[rs[i].Confidence] < rank[rs[j].Confidence]
		}
		return rs[i].Set < rs[j].Set
	})
}

// Summary counts resolutions by confidence.
func Summary(rs []Resolution) map[Confidence]int {
	out := map[Confidence]int{}
	for _, r := range rs {
		out[r.Confidence]++
	}
	return out
}

// ListSets returns the set directories in a pack, skipping dotted entries.
//
// The skip is not cosmetic: an untracked `.claude` directory is currently
// sitting in this pack's files/ tree, and counting it as a set made an earlier
// survey report 73 where the pack ships 72.
func ListSets(filesRoot string) ([]string, error) {
	entries, err := os.ReadDir(filesRoot)
	if err != nil {
		return nil, err
	}
	var sets []string
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			sets = append(sets, e.Name())
		}
	}
	sort.Strings(sets)
	if len(sets) == 0 {
		return nil, fmt.Errorf("no set directories under %s", filesRoot)
	}
	return sets, nil
}
