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

	// Root is the committed set directory, kept so a tie between candidate
	// directories can be settled by reading the icons rather than by the order
	// the walk happened to reach them. See Similarity.
	Root string
}

// Count returns how many icons the set ships.
func (f SetFingerprint) Count() int { return len(f.Hashes) }

// FingerprintSet reads a committed set directory.
//
// Keyed by the path relative to the set root, not by the leaf name. Several
// sets keep their upstream's subdirectory structure -- fontawesome vendors
// brands/, regular/ and solid/ -- and a leaf-name key counts one committed
// icon once for every variant directory that happens to contain the same name,
// so fontawesome scored 5,068 matches against a set of 2,284 and the best
// directory came out as the package root. Relative paths make the comparison
// structural, and a flat set is the case where the relative path is just the
// filename, so nothing is lost there.
func FingerprintSet(dir string) (SetFingerprint, error) {
	fp := SetFingerprint{Name: filepath.Base(dir), Hashes: map[string]string{}, Root: dir}

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
		rel, relErr := filepath.Rel(dir, path)
		if relErr != nil {
			return relErr
		}
		sum := sha256.Sum256(data)
		fp.Hashes[iconKey(rel)] = hex.EncodeToString(sum[:])
		return nil
	})
	return fp, err
}

// iconKey normalises a relative path to its comparison key: forward slashes,
// no extension, lower case. The extension goes because a few upstreams ship
// .SVG, and the case goes because two of the vendored sets differ from their
// upstream only in filename casing.
func iconKey(rel string) string {
	rel = filepath.ToSlash(rel)
	return strings.ToLower(strings.TrimSuffix(rel, filepath.Ext(rel)))
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

	// Depth is the total SVG count in the subtree, used only to prefer the
	// tightest directory among equally good ones.
	Depth int

	// Rivals are directories that scored identically, are not this one seen
	// from a different level, and hold *different bytes*. A non-empty list
	// with no byte matches means the scorer has no basis left for choosing --
	// see rivalsOf.
	Rivals []string

	// Content digests the matched icons, so two directories that tie can be
	// asked whether they actually differ.
	Content string

	// Similarity and Runner record a settled tie: how closely this directory
	// resembles the committed icons, and how closely the next best one did.
	// Both are zero when no tie arose.
	Similarity float64
	Runner     float64
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
	// The coverage floor is deliberately the same 90% the refresh's retention
	// guard uses. Below that the guard refuses to replace the directory
	// anyway, so a stricter manifest threshold only rejects entries the
	// refresh would have accepted, while admitting none it would refuse.
	// Measured, 95% excluded @mapbox/maki for maki-icons at 198 of 211 --
	// unmistakably the right project, rejected over thirteen retired icons.
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

	// ConfidenceAmbiguous means the set was matched, and matched equally well
	// by two or more directories that are not the same icons seen from
	// different depths. Coverage cannot choose between them and no file is
	// byte-identical, so there is nothing left to decide on -- picking one
	// would be picking whichever the directory walk reached last.
	//
	// It is graded separately from "partial" because the remaining work is
	// different in kind. A partial result needs a better candidate package; an
	// ambiguous one has the right package already and needs a human to say
	// which variant the pack vendored.
	ConfidenceAmbiguous Confidence = "ambiguous"

	// ConfidencePartial means some names line up but coverage is incomplete.
	// That is the dangerous middle: two icon sets from the same design
	// language share a great many names, so it may be a different project.
	ConfidencePartial Confidence = "partial"

	// ConfidenceNone means nothing lined up.
	ConfidenceNone Confidence = "none"
)

// PackageCoverageFloor is the name-coverage fraction at which an upstream
// counts as identified. It matches the refresh's MinRetention on purpose --
// see ConfidencePackage.
const PackageCoverageFloor = 0.90

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
	case c.Identical == 0 && len(c.Rivals) > 0:
		// Bytes are the only thing that could have separated these, and there
		// are none. Say so rather than returning a coin toss dressed as a
		// measurement.
		return ConfidenceAmbiguous
	case identical >= 0.80:
		return ConfidenceStrong
	case present >= PackageCoverageFloor:
		return ConfidencePackage
	default:
		return ConfidencePartial
	}
}

// ScoreDirectory compares a directory and everything under it against a set.
//
// Recursive, and that is the point. Several sets keep their upstream's
// subdirectory structure -- fontawesome ships brands/, regular/ and solid/ and
// the pack vendors all three -- so the directory that actually corresponds to
// the set is a *parent* holding no SVGs of its own. Scoring only direct
// children never sees it, and the best a non-recursive scan could report was
// one variant: 1,758 of fontawesome's 2,284 icons, graded "partial", when the
// real answer is svgs/ and covers all of them.
func ScoreDirectory(dir string, fp SetFingerprint) (Candidate, error) {
	c := Candidate{Dir: dir}
	var matched []string

	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // an unreadable subtree is skipped, not fatal
		}
		if d.IsDir() || !strings.HasSuffix(strings.ToLower(path), ".svg") {
			return nil
		}
		c.Depth++

		rel, relErr := filepath.Rel(dir, path)
		if relErr != nil {
			return nil //nolint:nilerr
		}
		want, known := fp.Hashes[iconKey(rel)]
		if !known {
			c.Extra++
			return nil
		}
		c.Present++

		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		sum := sha256.Sum256(data)
		got := hex.EncodeToString(sum[:])
		if got == want {
			c.Identical++
		}
		matched = append(matched, iconKey(rel)+":"+got)
		return nil
	})

	// A digest over the icons this directory shares with the set, so a tie can
	// be interrogated: two directories holding the same bytes are a packaging
	// duplicate and either will do, while two holding different bytes are a
	// choice nothing here is qualified to make.
	sort.Strings(matched)
	sum := sha256.Sum256([]byte(strings.Join(matched, "\n")))
	c.Content = hex.EncodeToString(sum[:])

	return c, err
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
	var scored []Candidate

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil //nolint:nilerr // an unreadable subtree is skipped, not fatal
		}
		c, err := ScoreDirectory(path, fp)
		if err != nil {
			return nil //nolint:nilerr
		}
		if c.Present > 0 {
			scored = append(scored, c)
		}
		// Strictly better wins, and on a tie the *deeper* directory does.
		// WalkDir visits a parent before its children, so accepting an equal
		// score with no more extras lets the tight directory replace the
		// package root whenever both cover the set equally. Without that every
		// set resolves to "." and the refresh copies a README's logo into the
		// pack alongside the icons.
		if c.Score() > best.Score() ||
			(c.Score() == best.Score() && c.Present > 0 && c.Extra <= best.Extra) {
			best = c
		}
		return nil
	})

	best = settleTie(best, scored, root, fp)

	if best.Dir != "" {
		if rel, err := filepath.Rel(root, best.Dir); err == nil {
			best.Dir = rel
		}
	}
	return best, err
}

// settleTie decides between directories that score identically.
//
// Name coverage has already said they are equal and no file is byte-identical,
// so the only question left is which of them the committed icons actually
// resemble. When one is clearly closest the tie is decided and the answer is
// a measurement; when the field is tight the result stays ambiguous and a
// human picks. Both outcomes beat the previous behaviour, which was to return
// whichever directory the walk reached last and call it a resolution.
func settleTie(best Candidate, scored []Candidate, root string, fp SetFingerprint) Candidate {
	rivals := rivalsOf(best, scored, root)
	if len(rivals) == 0 || best.Identical > 0 {
		best.Rivals = rivals
		return best
	}

	type ranked struct {
		cand  Candidate
		score float64
	}
	field := []ranked{{best, Similarity(best.Dir, fp)}}
	for _, c := range scored {
		for _, r := range rivals {
			if rel, err := filepath.Rel(root, c.Dir); err == nil && filepath.ToSlash(rel) == r {
				field = append(field, ranked{c, Similarity(c.Dir, fp)})
			}
		}
	}
	sort.Slice(field, func(i, j int) bool { return field[i].score > field[j].score })

	if len(field) > 1 && field[1].score > 0 && field[0].score >= field[1].score*TieBreakMargin {
		winner := field[0].cand
		winner.Similarity = field[0].score
		winner.Runner = field[1].score
		return winner
	}

	best.Rivals = rivals
	if len(field) > 0 {
		best.Similarity = field[0].score
	}
	return best
}

// rivalsOf lists the directories that score exactly as well as the winner and
// are not simply the winner nested inside a parent.
//
// A package shipping sibling variant directories -- cryptocurrency-icons has
// svg/{black,white,color,icon}, each with the same 483 filenames -- defeats
// name scoring completely: all four are equal, and which one "wins" is decided
// by the order WalkDir happens to visit them. The committed set is black with
// fill="currentColor" added, so the arbitrary pick was white, and a refresh
// would have replaced every themeable icon with a hard-coded #FFF one. Nothing
// downstream catches that: the file count is identical, so the retention guard
// sees a clean swap.
//
// A parent and the child it contains are excluded. Those tie constantly and
// the deeper-wins rule already handles them correctly -- they are the same
// icons seen from two levels, not two different answers.
func rivalsOf(best Candidate, scored []Candidate, root string) []string {
	if best.Dir == "" {
		return nil
	}
	var rivals []string
	for _, c := range scored {
		if c.Dir == best.Dir || c.Score() != best.Score() {
			continue
		}
		if nests(c.Dir, best.Dir) || nests(best.Dir, c.Dir) {
			continue
		}
		if c.Content == best.Content {
			// Same bytes under a second path. ionicons ships its icons at
			// dist/svg, dist/ionicons/svg and inside the Stencil collection
			// output -- three copies of one directory, where picking any of
			// them gives the identical refresh. Reporting that as a decision a
			// human must take would cost the set its manifest entry over
			// nothing.
			continue
		}
		if rel, err := filepath.Rel(root, c.Dir); err == nil {
			rivals = append(rivals, filepath.ToSlash(rel))
		}
	}
	sort.Strings(rivals)
	return rivals
}

// nests reports whether outer contains inner as a directory ancestor.
func nests(outer, inner string) bool {
	rel, err := filepath.Rel(outer, inner)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// Resolution is one set's answer.
type Resolution struct {
	Set        string     `json:"set"`
	Icons      int        `json:"icons"`
	Package    string     `json:"package,omitempty"`
	Version    string     `json:"version,omitempty"`
	Path       string     `json:"path,omitempty"`
	Matched    int        `json:"matched"`
	Extra      int        `json:"extra"`
	Rivals     []string   `json:"rivals,omitempty"`
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
