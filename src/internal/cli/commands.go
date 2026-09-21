package cli

import (
	"context"
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/ichava/maintainer-toolkit/src/internal/app"
	"github.com/ichava/maintainer-toolkit/src/internal/core/checker"
	"github.com/ichava/maintainer-toolkit/src/internal/core/config"
	"github.com/ichava/maintainer-toolkit/src/internal/core/pipeline"
	"github.com/ichava/maintainer-toolkit/src/internal/ui"
)

// ---------------------------------------------------------------- list-packs

func newListPacksCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "list-packs",
		Aliases: []string{"list"},
		Short:   "List the registered packs",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			packs, err := app.LoadPacks(configDir(), "")
			if err != nil {
				return failure(err)
			}

			if ui.JSONMode() {
				rows := make([]map[string]string, 0, len(packs))
				for _, p := range packs {
					rows = append(rows, map[string]string{
						"name": p.Name, "pack": p.Pack,
						"current": config.ResolvedCurrentVersion(p),
						"source":  p.Source.Type,
					})
				}
				return ui.JSON(rows)
			}

			ui.Printf("Registered packs (%d):\n", len(packs))
			for _, p := range packs {
				// The version shown is the resolved one -- what the pack repo
				// records, not this repository's fallback. The Python printed
				// the fallback, so a synced pack still displayed a stale
				// number here while `check` correctly said it was up to date.
				current := config.ResolvedCurrentVersion(p)
				if current == "" {
					current = "?"
				}
				ui.Printf("  %s %s -> %s (current: %s)\n", ui.Sym.Bullet, p.Name, p.Pack, current)
			}
			return nil
		},
	}
}

// --------------------------------------------------------------------- check

func newCheckCommand() *cobra.Command {
	var pack string

	cmd := &cobra.Command{
		Use:   "check",
		Short: "Check every pack against its upstream registry",
		Long: `Check every pack against its upstream registry.

Exits 1 when any pack is behind upstream, so it can gate a scheduled job.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			packs, err := app.LoadPacks(configDir(), pack)
			if err != nil {
				return failure(err)
			}

			results := app.CheckAll(cmd.Context(), packs)

			if ui.JSONMode() {
				if err := ui.JSON(results); err != nil {
					return failure(err)
				}
			} else {
				ui.Printf("%s", renderCheck(results))
				ui.Summarise(results)
			}

			// Exit 1 on any stale pack. Load-bearing for CI.
			if checker.AnyStale(results) {
				return &ExitError{Code: ExitFailure}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&pack, "pack", "", "restrict to one pack slug")
	return cmd
}

// renderCheck maps results onto the shared table renderer in internal/ui.
func renderCheck(results []checker.Result) string {
	return ui.RenderCheckTable(ui.CheckRows(results))
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// ---------------------------------------------------------------------- sync

func newSyncCommand() *cobra.Command {
	var (
		pack     string
		allPacks bool
		dryRun   bool
		force    bool
	)

	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Refresh a pack's assets from upstream and open a pull request",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if pack == "" && !allPacks {
				return usageErr("pass --pack=<slug> or --all")
			}
			packs, err := app.LoadPacks(configDir(), pack)
			if err != nil {
				return failure(err)
			}
			return runSync(cmd.Context(), packs, dryRun, force)
		},
	}
	f := cmd.Flags()
	f.StringVar(&pack, "pack", "", "sync one pack slug")
	f.BoolVar(&allPacks, "all", false, "sync every pack")
	f.BoolVar(&dryRun, "dry-run", false, "skip the git commit and pull request")
	f.BoolVar(&force, "force", false, "refresh even when not stale")
	return cmd
}

func runSync(ctx context.Context, packs []*config.PackConfig, dryRun, force bool) error {
	c := checker.New()
	failed := false

	for _, p := range packs {
		result := c.CheckPack(ctx, p)

		// An unresolvable upstream is reported and skipped, not fatal: one
		// dead registry must not abort a run across every other pack.
		if result.Latest == "" {
			ui.Warning("%s: %s", p.Pack, result.Reason)
			continue
		}
		if !result.Stale && !force {
			ui.Success("%s: up-to-date (%s)", p.Pack, result.Current)
			continue
		}

		ui.Info("%s: refreshing from %s %s %s", p.Pack, orDash(result.Current), ui.Sym.Arrow, result.Latest)

		outcome, err := RunRecipe(ctx, p, result.Latest, dryRun)
		if err != nil {
			// The literal "recipe pending" is a cross-repository contract:
			// icon-sets-bundled's sync-upstream.yml greps for it and turns
			// this into a green run with a ::notice::, because that pack's
			// recipe genuinely does not exist yet.
			if errors.Is(err, pipeline.ErrRecipePending) {
				ui.Warning("%s: %s", p.Pack, err)
				failed = true
				continue
			}
			ui.Failure("%s: %s", p.Pack, err)
			failed = true
			continue
		}
		if !outcome.Success {
			ui.Failure("%s: %s", p.Pack, outcome.Error())
			failed = true
			continue
		}
		ui.Success("%s: done -- %v", p.Pack, outcome.Summary())
	}

	if failed {
		return &ExitError{Code: ExitFailure}
	}
	return nil
}

// --------------------------------------------------------------------- recipe

func newRecipeCommand() *cobra.Command {
	var (
		versionFlag string
		dryRun      bool
	)

	cmd := &cobra.Command{
		Use:   "recipe <name>",
		Short: "Run one pack's recipe directly, skipping the upstream check",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			pack, err := config.LoadPack(args[0], configDir())
			if err != nil {
				return failure(err)
			}

			resolved := versionFlag
			if resolved == "" {
				resolved = pack.CurrentVersion
			}
			if resolved == "" {
				return usageErr("%s: no version supplied and no current_version in config", pack.Pack)
			}

			outcome, err := RunRecipe(cmd.Context(), pack, resolved, dryRun)
			if err != nil {
				return failure(err)
			}
			ui.Printf("%v\n", outcome.Summary())
			if !outcome.Success {
				return &ExitError{Code: ExitFailure}
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&versionFlag, "version", "", "version override")
	f.BoolVar(&dryRun, "dry-run", false, "skip the git commit and pull request")
	return cmd
}

// RunRecipe dispatches a pack to its recipe.
//
// Dispatch order is by pack name first and source type second, and that is not
// incidental: icon-sets-emoji declares source.type "url", so checking the type
// first would send it to the "no recipe wired" branch.
func RunRecipe(ctx context.Context, pack *config.PackConfig, version string, dryRun bool) (pipeline.Result, error) {
	switch {
	case pack.Name == "icon-sets-emoji":
		return pipeline.Result{}, fmt.Errorf(
			"%s: the emoji-sets recipe is not ported to Go yet; the Python entrypoint still runs it", pack.Pack)

	case pack.Name == "icon-sets-bundled":
		return pipeline.Result{}, fmt.Errorf("%s: bundled-icons %w", pack.Pack, pipeline.ErrRecipePending)

	case pack.Source.Type == "npm":
		return pipeline.Result{}, fmt.Errorf(
			"%s: the simple-npm recipe is not ported to Go yet; the Python entrypoint still runs it", pack.Pack)
	}

	return pipeline.Result{}, fmt.Errorf(
		"%s: no recipe wired for source.type=%q", pack.Pack, pack.Source.Type)
}
