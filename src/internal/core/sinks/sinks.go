// Package sinks writes the in-flight tree to its destination.
//
// Order matters and is set by the recipes: Filesystem, then VersionStamp, then
// GitBranch. Stamping after the commit would record a version that never
// shipped, and stamping into this repository rather than the pack repo is what
// made the sync non-convergent before (V49) -- the bump went into the runner's
// throwaway checkout and every run saw the same stale value.
package sinks

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ichava/maintainer-toolkit/src/internal/core/config"
	"github.com/ichava/maintainer-toolkit/src/internal/core/gitx"
	"github.com/ichava/maintainer-toolkit/src/internal/core/pipeline"
)

// Filesystem copies the refreshed SVGs into the pack repo.
type Filesystem struct {
	pipeline.SinkKind

	// Root is the destination, with {pack_root} and {version} interpolated.
	Root string

	// Incremental keeps what is already there. Off by default, which means
	// the destination is wiped first -- that is what removes an icon upstream
	// has deleted. An incremental sink would leave it in the pack forever.
	Incremental bool

	// MinRetention refuses to replace a populated destination with
	// substantially fewer files, as a fraction of what is already there.
	// Zero disables the check.
	//
	// Because the wipe is the point, it is also the hazard. Observed while
	// building the aggregator refresh: iconoir's upstream reorganised into
	// per-weight directories, so a refresh matching one of them would have
	// replaced 1,671 committed icons with 1,383 -- deleting 288 without
	// anything failing. A pack losing a sixth of a set to an upstream
	// reshuffle should stop the run and ask, not proceed quietly on a Monday
	// morning cron.
	MinRetention float64

	Log *slog.Logger
}

func (Filesystem) Name() string { return "Filesystem" }

func (s Filesystem) Execute(ctx *pipeline.Context) error {
	source, ok := ctx.FetchedPath()
	if !ok {
		return fmt.Errorf("Filesystem sink: fetched_path not set; needs a source upstream")
	}

	version, _ := ctx.FetchedVersion()
	target := config.Interpolate(s.Root, map[string]string{
		"version":   version,
		"pack_root": ctx.ConfigString(pipeline.ConfigPackRoot),
	})
	target, err := filepath.Abs(target)
	if err != nil {
		return err
	}

	log := s.Log
	if log == nil {
		log = slog.Default()
	}

	if !s.Incremental {
		if _, err := os.Stat(target); err == nil {
			if err := s.guardRetention(source, target, log); err != nil {
				return err
			}
			log.Info("filesystem: wiping", "target", target)
			if err := os.RemoveAll(target); err != nil {
				return err
			}
		}
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		return err
	}

	var files []string
	err = filepath.WalkDir(source, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(strings.ToLower(path), ".svg") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return err
	}
	sort.Strings(files)

	copied := 0
	for _, src := range files {
		rel, err := filepath.Rel(source, src)
		if err != nil {
			return err
		}
		dest := filepath.Join(target, rel)
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		if err := copyFile(src, dest); err != nil {
			return err
		}
		copied++
	}

	ctx.Metric("filesystem", map[string]any{"copied": copied, "target": target})
	ctx.SetString(pipeline.KeyFilesystemTgt, target)
	log.Info("filesystem", "copied", copied, "target", target)
	return nil
}

// guardRetention refuses a wipe that would shrink the destination too far.
//
// Counted before anything is removed, so a refusal leaves the pack exactly as
// it was.
func (s Filesystem) guardRetention(source, target string, log *slog.Logger) error {
	if s.MinRetention <= 0 {
		return nil
	}

	existing, err := countSVGs(target)
	if err != nil || existing == 0 {
		return nil //nolint:nilerr // nothing to protect
	}
	incoming, err := countSVGs(source)
	if err != nil {
		return err
	}

	ratio := float64(incoming) / float64(existing)
	if ratio >= s.MinRetention {
		if incoming < existing {
			// A small shrink is ordinary -- upstreams retire icons -- but it
			// is worth saying out loud, because nothing else in the run will.
			log.Warn("filesystem: destination shrinks",
				"target", target, "from", existing, "to", incoming)
		}
		return nil
	}

	return fmt.Errorf(
		"refusing to replace %d icons in %s with %d (%.0f%% of what is there, floor is %.0f%%): "+
			"upstream has probably reorganised, so check the manifest path before letting this through",
		existing, target, incoming, ratio*100, s.MinRetention*100)
}

func countSVGs(dir string) (int, error) {
	n := 0
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(strings.ToLower(path), ".svg") {
			n++
		}
		return nil
	})
	return n, err
}

// copyFile copies contents and mode. Only .svg files reach it -- anything else
// in the upstream tree is deliberately not carried into the pack.
func copyFile(src, dest string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	info, err := in.Stat()
	if err != nil {
		return err
	}

	out, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// VersionStamp records the fetched version in the pack repo's own config.
//
// Into the pack repo, never back into this one. The GitBranch sink commits it
// alongside the assets, so the version and the SVGs it describes move in one
// commit and the next run compares against what actually shipped.
type VersionStamp struct {
	pipeline.SinkKind

	Pack *config.PackConfig
	Log  *slog.Logger
}

func (VersionStamp) Name() string { return "VersionStamp" }

func (s VersionStamp) Execute(ctx *pipeline.Context) error {
	log := s.Log
	if log == nil {
		log = slog.Default()
	}

	version, ok := ctx.FetchedVersion()
	if !ok || version == "" {
		ctx.Metric("version_stamp", map[string]any{"status": "no-version"})
		return nil
	}

	path, err := config.WriteVendoredVersion(s.Pack, version)
	if err != nil {
		return err
	}
	if path == "" {
		// Not a failure: a pack with no version file simply has nothing to
		// stamp, and the sync will re-detect the same version next run.
		log.Warn("version-stamp: pack ships no version file, so the sync will re-detect",
			"pack", s.Pack.Pack, "file", s.Pack.VersionFile, "version", version)
		ctx.Metric("version_stamp", map[string]any{"status": "no-file"})
		return nil
	}

	ctx.Metric("version_stamp", map[string]any{
		"status": "stamped", "version": version, "file": path,
	})
	log.Info("version-stamp", "pack", s.Pack.Pack, "version", version, "file", path)
	return nil
}

// GitBranch commits the refreshed pack and opens a pull request.
type GitBranch struct {
	pipeline.SinkKind

	RepoRoot      string
	Branch        string
	CommitSubject string
	CommitBody    string
	Base          string
	OpenPR        bool

	Git *gitx.Git
	Log *slog.Logger
}

func (GitBranch) Name() string { return "GitBranch" }

// Defaults matching the Python templates.
const (
	DefaultBranch        = "chore/sync-upstream-{version}"
	DefaultCommitSubject = "chore(upstream): bump {pack} to v{new}"
	DefaultCommitBody    = "Automated sync from upstream {pack} {old} -> {new}.\n\n" +
		"Generated by ichava/maintainer-toolkit. See documentation/icon-pack-maintainer-sync.md."
)

func (s GitBranch) Execute(ctx *pipeline.Context) error {
	log := s.Log
	if log == nil {
		log = slog.Default()
	}
	g := s.Git
	if g == nil {
		g = gitx.New()
	}

	version, _ := ctx.FetchedVersion()
	pack := ctx.ConfigString(pipeline.ConfigPack)
	if pack == "" {
		pack = ctx.PipelineName
	}
	old := ctx.ConfigString(pipeline.ConfigOldVersion)
	if old == "" {
		old = "unknown"
	}

	bindings := map[string]string{
		"version":   version,
		"pack_root": ctx.ConfigString(pipeline.ConfigPackRoot),
		"pack":      pack,
		"old":       old,
		"new":       version,
	}

	repo, err := filepath.Abs(config.Interpolate(s.RepoRoot, bindings))
	if err != nil {
		return err
	}

	runCtx := context.Background()

	dirty, err := g.HasUncommittedChanges(runCtx, repo)
	if err != nil {
		return err
	}
	if !dirty {
		// Nothing changed. Upstream published a version whose assets are
		// byte-identical to what the pack already ships, which happens and is
		// not an error.
		log.Info("git-branch: no diff, skipping", "repo", repo)
		ctx.Metric("git_branch", map[string]any{"status": "no-diff"})
		return nil
	}

	branch := config.Interpolate(orDefault(s.Branch, DefaultBranch), bindings)
	subject := config.Interpolate(orDefault(s.CommitSubject, DefaultCommitSubject), bindings)
	body := config.Interpolate(orDefault(s.CommitBody, DefaultCommitBody), bindings)

	// Fetch the tracking ref before pushing. actions/checkout fetches only the
	// default branch, so refs/remotes/origin/<branch> does not exist on a
	// runner -- and --force-with-lease then has nothing to compare and git
	// rejects the push as "stale info". A missing branch upstream is fine, so
	// the error is logged rather than returned.
	if err := g.FetchBranch(runCtx, repo, branch); err != nil {
		log.Debug("git-branch: no existing remote branch to fetch", "branch", branch, "error", err)
	}

	if err := g.CreateBranch(runCtx, repo, branch); err != nil {
		return err
	}
	sha, err := g.CommitAll(runCtx, repo, subject, body)
	if err != nil {
		return err
	}
	if err := g.PushBranch(runCtx, repo, branch, true); err != nil {
		return err
	}

	prURL := ""
	if s.OpenPR {
		prURL, err = g.OpenPR(runCtx, repo, branch, subject, body, s.Base)
		if err != nil {
			return err
		}
	}

	ctx.Metric("git_branch", map[string]any{
		"status": "committed", "branch": branch, "sha": sha, "pr_url": prURL,
	})
	ctx.SetString(pipeline.KeyGitBranch, branch)
	ctx.SetString(pipeline.KeyGitSHA, sha)
	if prURL != "" {
		ctx.SetString(pipeline.KeyPRURL, prURL)
	}

	short := sha
	if len(short) > 8 {
		short = short[:8]
	}
	log.Info("git-branch: committed", "sha", short, "branch", branch, "pr", prURL)
	return nil
}

func orDefault(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}
