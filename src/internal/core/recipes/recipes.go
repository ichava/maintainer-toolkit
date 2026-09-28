// Package recipes composes a pack's pipelines.
//
// A recipe builds a pipeline; it does not run one. The caller runs it, which
// is what lets the TUI show the stages before anything is fetched and lets a
// dry run skip the sinks that commit.
package recipes

import (
	"fmt"

	"github.com/ichava/maintainer-toolkit/src/internal/core/config"
	"github.com/ichava/maintainer-toolkit/src/internal/core/pipeline"
	"github.com/ichava/maintainer-toolkit/src/internal/core/sinks"
	"github.com/ichava/maintainer-toolkit/src/internal/core/sources"
	"github.com/ichava/maintainer-toolkit/src/internal/core/transforms"
)

// Build returns the pipelines for a pack, in the order they must run.
//
// Dispatch is on the pack NAME first and the source type second, and reversing
// that breaks icon-sets-emoji: it declares source.type "url" -- because that is
// how its *version* is discovered -- while its assets come from three separate
// upstreams that only the named recipe knows about.
func Build(pack *config.PackConfig, version string, dryRun bool, configDir string) ([]*pipeline.Pipeline, error) {
	switch {
	case pack.Name == "icon-sets-emoji":
		return buildEmojiSets(pack, version, dryRun)

	case pack.Name == "icon-sets-bundled":
		// A native per-set refresh, not the `iconify` one the pack config
		// asks for -- see buildBundled for why that would have been a rewrite
		// rather than a refresh.
		//
		// This no longer returns ErrRecipePending, which icon-sets-bundled's
		// sync-upstream.yml greps for to turn the failure into a green run.
		// That wrapper becomes dead once a manifest exists, and should be
		// removed from the workflow in the same change that ships one --
		// leaving it costs nothing, but it will never fire again.
		return buildBundled(pack, configDir, dryRun)

	case pack.Source.Type == "npm":
		p, err := buildSimpleNPM(pack, version, dryRun)
		if err != nil {
			return nil, err
		}
		return []*pipeline.Pipeline{p}, nil
	}

	return nil, fmt.Errorf("%s: no recipe wired for source.type=%q", pack.Pack, pack.Source.Type)
}

// packConfig is the config overlay every recipe supplies to its stages.
func packConfig(pack *config.PackConfig) map[string]any {
	old := pack.CurrentVersion
	if vendored := config.ReadVendoredVersion(pack); vendored != "" {
		// Prefer what the pack actually ships, so the commit message names the
		// version being replaced rather than this repository's stale fallback.
		old = vendored
	}
	if old == "" {
		old = "unknown"
	}
	return map[string]any{
		pipeline.ConfigPack:       pack.Pack,
		pipeline.ConfigPackRoot:   pack.PackRoot,
		pipeline.ConfigOldVersion: old,
	}
}

// buildSimpleNPM handles a pack whose assets are one npm package.
//
// icon-sets-tabler and icon-sets-flag both use this: fetch the tarball, narrow
// to the icons directory, apply the policy, copy into the pack, stamp, commit.
func buildSimpleNPM(pack *config.PackConfig, version string, dryRun bool) (*pipeline.Pipeline, error) {
	if pack.Source.Type != "npm" {
		return nil, fmt.Errorf("simple-npm: source.type is %q, not npm", pack.Source.Type)
	}
	if pack.Source.Package == "" {
		return nil, fmt.Errorf("simple-npm: source.package is required")
	}

	p := pipeline.Named(fmt.Sprintf("%s@%s", pack.Name, version)).
		Config(packConfig(pack)).
		Source(sources.NpmTarball{Package: pack.Source.Package, Version: version})

	if pack.Source.SourcePath != "" {
		p.Transform(transforms.SubsetTo{Subdir: pack.Source.SourcePath})
	}
	p.Transform(transforms.Sanitise{})

	for _, sink := range pack.Sinks {
		switch sink.Type {
		case "filesystem":
			p.Sink(sinks.Filesystem{Root: sink.Root})

		case "git-branch":
			if dryRun {
				continue
			}
			// Stamp before commit, always. The version has to be in the tree
			// the commit captures, or the pack ships assets whose recorded
			// version is the previous one and every later run re-proposes the
			// same refresh.
			p.Sink(sinks.VersionStamp{Pack: pack})
			p.Sink(sinks.GitBranch{
				RepoRoot: orDefault(sink.RepoRoot, pack.PackRoot),
				Branch:   orDefault(sink.Branch, sinks.DefaultBranch),
				OpenPR:   true,
			})
		}
	}
	return p, nil
}

// Emoji recipe defaults.
//
// These are not arbitrary. unicode_version once defaulted ahead of what
// Unicode had published, so every taxonomy fetch 404'd and the pack shipped a
// tagged release containing zero SVGs. Move them only to a version that exists.
const (
	DefaultOpenMojiVersion = "15.1.0"
	DefaultUnicodeVersion  = "16.0"
)

// buildEmojiSets composes the three-upstream emoji pack.
//
// Twemoji from npm, OpenMoji colour and black from one GitHub archive each,
// all categorised against the same Unicode taxonomy, then a fourth
// commit-only pipeline so the three asset runs land in a single commit rather
// than three.
func buildEmojiSets(pack *config.PackConfig, twemojiVersion string, dryRun bool) ([]*pipeline.Pipeline, error) {
	if twemojiVersion == "" {
		return nil, fmt.Errorf("emoji-sets: a twemoji version is required")
	}

	filesRoot := pack.PackRoot + "/resources/assets/svg/files"
	cfg := packConfig(pack)
	cldr := sources.UnicodeCldr{UnicodeVersion: DefaultUnicodeVersion}

	twemoji := pipeline.Named(fmt.Sprintf("emoji-sets:twemoji@%s", twemojiVersion)).
		Config(cfg).
		Source(sources.Compound{
			First:  cldr,
			Second: sources.NpmTarball{Package: "@twemoji/svg", Version: twemojiVersion},
		}).
		// Twemoji ships a class on every path; it is the one upstream where
		// stripping it is wanted, so the pack's own styling applies.
		Transform(transforms.Sanitise{AlsoStripClass: true}).
		Transform(transforms.Categorise{By: "cldr"}).
		Transform(transforms.Indexer{Targets: []string{"codepoints", "names"}}).
		Sink(sinks.Filesystem{Root: filesRoot + "/twemoji"})

	openmoji := func(name, subdir string) *pipeline.Pipeline {
		return pipeline.Named(fmt.Sprintf("emoji-sets:%s@%s", name, DefaultOpenMojiVersion)).
			Config(cfg).
			Source(sources.Compound{
				First: cldr,
				Second: sources.GithubArchive{
					// OpenMoji tags without a `v`, so the built-in template
					// would ask for a tag that does not exist.
					ArchiveURL: "https://github.com/hfg-gmuend/openmoji/archive/refs/tags/{version}.zip",
					Owner:      "hfg-gmuend", Repo: "openmoji",
					Version: DefaultOpenMojiVersion,
				},
			}).
			Transform(transforms.SubsetTo{Subdir: subdir}).
			Transform(transforms.Sanitise{}).
			Transform(transforms.Categorise{By: "cldr"}).
			Sink(sinks.Filesystem{Root: filesRoot + "/" + name})
	}

	pipelines := []*pipeline.Pipeline{
		twemoji,
		openmoji("openmoji-color", "color/svg"),
		openmoji("openmoji-black", "black/svg"),
	}

	if !dryRun {
		pipelines = append(pipelines,
			pipeline.Named("emoji-sets:commit").
				Config(cfg).
				// Carries the twemoji version forward. Each pipeline has its
				// own context, so without this the stamp finds no version and
				// silently does nothing -- which is what the Python did, and
				// why this pack never converged.
				Source(sources.Static{Version: twemojiVersion}).
				Sink(sinks.VersionStamp{Pack: pack}).
				Sink(sinks.GitBranch{RepoRoot: pack.PackRoot, OpenPR: true}))
	}
	return pipelines, nil
}

func orDefault(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}
