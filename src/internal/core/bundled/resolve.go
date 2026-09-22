package bundled

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ichava/maintainer-toolkit/src/internal/core/sources"
)

// Fetcher retrieves an npm package into a directory and reports its version.
type Fetcher interface {
	Fetch(ctx context.Context, pkg, dest string) (version string, err error)
}

// NpmFetcher shells out to npm pack, the same way the sources package does.
type NpmFetcher struct{ Log *slog.Logger }

// Fetch downloads and extracts a package, returning the version npm resolved.
func (f NpmFetcher) Fetch(ctx context.Context, pkg, dest string) (string, error) {
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return "", err
	}

	cmd := exec.CommandContext(ctx, "npm", "pack", pkg, "--silent")
	cmd.Dir = dest
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("npm pack %s: %s", pkg, firstLine(string(ee.Stderr)))
		}
		return "", fmt.Errorf("npm pack %s: %w", pkg, err)
	}

	tarball := ""
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			tarball = line
		}
	}
	if tarball == "" {
		return "", fmt.Errorf("npm pack %s printed no tarball name", pkg)
	}

	if err := sources.ExtractTarGz(filepath.Join(dest, tarball), filepath.Join(dest, "x")); err != nil {
		return "", err
	}
	return versionFromTarball(tarball, pkg), nil
}

// versionFromTarball reads the version out of the filename npm produced.
//
// `npm pack` names the file <name>-<version>.tgz, scope stripped and slashes
// replaced by a dash, so bootstrap-icons-1.11.3.tgz and
// vscode-codicons-0.0.36.tgz. Taking it from the filename avoids a second
// registry round trip per candidate, and there are several candidates per set
// across 72 sets.
func versionFromTarball(tarball, pkg string) string {
	base := strings.TrimSuffix(tarball, ".tgz")

	name := pkg
	if i := strings.Index(name, "/"); i >= 0 {
		name = strings.TrimPrefix(name[:i], "@") + "-" + name[i+1:]
	}
	if v := strings.TrimPrefix(base, name+"-"); v != base {
		return v
	}

	// Fall back to the trailing dash-separated component that looks numeric.
	if i := strings.LastIndex(base, "-"); i >= 0 && i+1 < len(base) {
		if c := base[i+1]; c >= '0' && c <= '9' {
			return base[i+1:]
		}
	}
	return ""
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// ResolveSet identifies the upstream for one committed set.
//
// Each candidate package is fetched and every directory in it scored against
// the committed icons. The first candidate that reproduces the set exactly
// wins immediately; otherwise the best-scoring one across all candidates is
// reported with its confidence, so a partial match is visible rather than
// silently accepted.
func ResolveSet(ctx context.Context, setDir string, fetch Fetcher, workDir string, log *slog.Logger, candidates ...string) Resolution {
	if log == nil {
		log = slog.Default()
	}

	fp, err := FingerprintSet(setDir)
	if err != nil {
		return Resolution{Set: filepath.Base(setDir), Confidence: ConfidenceNone, Note: err.Error()}
	}

	res := Resolution{Set: fp.Name, Icons: fp.Count(), Confidence: ConfidenceNone}
	if fp.Count() == 0 {
		res.Note = "set directory holds no SVGs"
		return res
	}

	// An explicit candidate replaces the guesses entirely. That is the loop a
	// human uses for the sets the name patterns cannot reach: supply a package,
	// and the tool answers whether its bytes are the ones already committed.
	if len(candidates) == 0 {
		candidates = CandidatePackages(fp.Name)
	}

	var tried []string
	for _, pkg := range candidates {
		dest := filepath.Join(workDir, fp.Name, sanitise(pkg))

		version, err := fetch.Fetch(ctx, pkg, dest)
		if err != nil {
			tried = append(tried, pkg)
			continue
		}

		// Search inside the npm wrapper, not above it. Every npm tarball nests
		// its contents in package/, and NpmTarball hands the *inside* of that
		// to the rest of the pipeline -- so a path recorded relative to the
		// extraction root is off by one component and SubsetTo cannot find it.
		// The audit and the recipe have to express paths in the same frame.
		root := filepath.Join(dest, "x", "package")
		if info, statErr := os.Stat(root); statErr != nil || !info.IsDir() {
			root = filepath.Join(dest, "x")
		}

		best, err := BestDirectory(root, fp)
		if err != nil || best.Present == 0 {
			tried = append(tried, pkg+"@"+version)
			continue
		}

		confidence := Classify(best, fp.Count())
		better := best.Identical > res.Identical ||
			(best.Identical == res.Identical && best.Present > res.Matched)

		if better {
			res.Package, res.Version, res.Path = pkg, version, best.Dir
			res.Matched, res.Identical, res.Confidence = best.Present, best.Identical, confidence
			res.Extra = best.Extra
			res.Rivals = best.Rivals
		}

		if confidence == ConfidenceExact {
			log.Info("resolved exactly", "set", fp.Name, "package", pkg, "path", best.Dir)
			return res
		}
	}

	if res.Package == "" && len(tried) > 0 {
		res.Note = "no candidate reproduced any icon; tried " + strings.Join(tried, ", ")
	}
	switch res.Confidence {
	case ConfidenceAmbiguous:
		res.Note = fmt.Sprintf(
			"%s is the right package (%d of %d icons present by name), but %q ties with %s and no file "+
				"is byte-identical -- these are variant directories sharing filenames, so nothing here "+
				"can tell them apart; pick the variant the pack vendored and set the path by hand",
			res.Package, res.Matched, res.Icons, res.Path, strings.Join(quoteAll(res.Rivals), ", "))
	case ConfidencePackage:
		res.Note = fmt.Sprintf(
			"upstream identified (%d of %d icons present by name) but no byte matches -- the pack vendored an older release; pin the version",
			res.Matched, res.Icons)
	case ConfidencePartial:
		res.Note = fmt.Sprintf(
			"only %d of %d icons present by name and %d byte-identical -- confirm this is the right project before trusting it",
			res.Matched, res.Icons, res.Identical)
	}
	return res
}

// sanitise makes a package name safe as a directory component.
func sanitise(pkg string) string {
	return strings.NewReplacer("/", "-", "@", "").Replace(pkg)
}

// quoteAll quotes each entry so a directory list reads unambiguously in a note.
func quoteAll(in []string) []string {
	out := make([]string, len(in))
	for i, v := range in {
		out[i] = strconv.Quote(v)
	}
	return out
}
