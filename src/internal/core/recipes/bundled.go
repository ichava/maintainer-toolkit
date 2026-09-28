package recipes

import (
	"fmt"
	"path/filepath"

	"github.com/ichava/maintainer-toolkit/src/internal/core/bundled"
	"github.com/ichava/maintainer-toolkit/src/internal/core/config"
	"github.com/ichava/maintainer-toolkit/src/internal/core/pipeline"
	"github.com/ichava/maintainer-toolkit/src/internal/core/sinks"
	"github.com/ichava/maintainer-toolkit/src/internal/core/sources"
	"github.com/ichava/maintainer-toolkit/src/internal/core/transforms"
)

// buildBundled composes the aggregator pack's refresh: one pipeline per set,
// each from that set's own upstream, plus a trailing commit pipeline.
//
// This is a *native* refresh, not the `iconify` one the pack config asks for.
// The committed icons are each upstream's own SVG distribution -- measured,
// 26 distinct <svg> attribute signatures across the pack -- where anything
// rendered from @iconify-json/* would have exactly one. Refreshing through
// Iconify would rewrite all 121,314 files into a uniform dialect and discard,
// among much else, fontawesome's embedded licence attribution.
//
// Two properties follow from that and are the whole design:
//
//   - Each set goes back to its own upstream, so its dialect is preserved and
//     a refresh is a refresh rather than a migration.
//   - A set with no manifest entry is left completely alone. Not fetched, not
//     wiped, not touched. An aggregator pack that quietly stopped refreshing a
//     third of itself would be bad; one that quietly *emptied* a third of
//     itself would be unrecoverable from a scheduled job.
func buildBundled(pack *config.PackConfig, configDir string, dryRun bool) ([]*pipeline.Pipeline, error) {
	manifest, err := bundled.LoadManifest(configDir, pack.Name)
	if err != nil {
		return nil, err
	}
	if manifest.Pack != "" && manifest.Pack != pack.Pack {
		return nil, fmt.Errorf("set manifest names %q but this pack is %q", manifest.Pack, pack.Pack)
	}

	filesRoot := filepath.Join(pack.PackRoot, "resources", "assets", "svg", "files")
	cfg := packConfig(pack)

	var pipelines []*pipeline.Pipeline
	for _, name := range manifest.SetNames() {
		src := manifest.Sets[name]

		version := src.Version
		if version == "" {
			version = "latest"
		}

		p := pipeline.Named(fmt.Sprintf("%s:%s@%s", pack.Name, name, version)).
			Config(cfg).
			Source(sources.NpmTarball{Package: src.Package, Version: version})

		switch {
		case len(src.Variants) > 0:
			// A flat set vendored from a multi-variant upstream. SubsetTo
			// cannot express this: it would pick one variant, and because the
			// sink wipes the destination first, the other variants' icons
			// would be deleted rather than left alone.
			p.Transform(transforms.FlattenVariants{
				Variants:  flattenVariants(src.Variants),
				Separator: src.Separator,
			})
		case src.Path != "":
			p.Transform(transforms.SubsetTo{Subdir: src.Path})
		}

		// Report, do not enforce. This pack has never been sanitised: its
		// icons keep comments the policy strips, and every fontawesome file
		// sampled changes under it. Enforcing here would turn a refresh into a
		// rewrite of tens of thousands of files and drop a licence notice, so
		// the violations are counted and surfaced and the bytes are left as
		// upstream shipped them. Turning this on is a decision for the pack's
		// owner, taken once, not a side effect of a Monday cron.
		p.Transform(transforms.Sanitise{ReportOnly: true})

		// Per-set destination. The sink wipes it first, which is what removes
		// an icon upstream has deleted -- and is exactly why an unresolved set
		// must never reach this point with a guessed package.
		// 90%: an upstream retiring a handful of icons is ordinary and passes
		// with a warning; one that reorganised its directories, so the manifest
		// path now matches only a fraction of the set, stops the run. That is
		// not hypothetical -- iconoir would have gone from 1,671 icons to
		// 1,383 under exactly that reshuffle.
		p.Sink(sinks.Filesystem{
			Root:         filepath.Join(filesRoot, name),
			MinRetention: bundled.PackageCoverageFloor,
		})

		pipelines = append(pipelines, p)
	}

	if !dryRun {
		// One commit for the whole refresh rather than one per set: 72 sets
		// would otherwise open 72 pull requests against the same pack.
		//
		// The stamped version is the pack's own upstream marker. Unlike the
		// single-upstream packs there is no one version to record here -- each
		// set has its own, and those live in the manifest -- so this carries
		// the aggregate version the pack already tracks.
		version := config.ResolvedCurrentVersion(pack)
		pipelines = append(pipelines,
			pipeline.Named(pack.Name+":commit").
				Config(cfg).
				Source(sources.Static{Version: version}).
				Sink(sinks.VersionStamp{Pack: pack}).
				Sink(sinks.GitBranch{RepoRoot: pack.PackRoot, OpenPR: true}))
	}

	return pipelines, nil
}

// flattenVariants converts the manifest's variants into the transform's.
//
// The two types are deliberately separate: transforms is a generic layer and
// has no business importing this pack's manifest schema.
func flattenVariants(in []bundled.Variant) []transforms.Variant {
	out := make([]transforms.Variant, len(in))
	for i, v := range in {
		out[i] = transforms.Variant{Subdir: v.Path, Prefix: v.Prefix, Suffix: v.Suffix}
	}
	return out
}
