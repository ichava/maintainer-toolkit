package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/spf13/cobra"

	"github.com/ichava/maintainer-toolkit/src/internal/core/bundled"
	"github.com/ichava/maintainer-toolkit/src/internal/core/config"
	"github.com/ichava/maintainer-toolkit/src/internal/ui"
)

// newBundledCommand groups the aggregator pack's own tooling.
//
// icon-sets-bundled is not like the other packs. It vendors 121,314 icons
// across 72 sets, each from a different upstream in that upstream's own SVG
// dialect -- 26 distinct <svg> attribute signatures across the pack -- and
// nothing records where any of them came from. Refreshing it therefore needs a
// per-set manifest that does not exist, and `audit` is what builds one.
func newBundledCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bundled",
		Short: "Tooling for the icon-sets-bundled aggregator pack",
	}
	cmd.AddCommand(newBundledAuditCommand())
	return cmd
}

func newBundledAuditCommand() *cobra.Command {
	var (
		only     string
		pkg      string
		workDir  string
		parallel int
	)

	cmd := &cobra.Command{
		Use:   "audit",
		Short: "Identify, by measurement, which upstream each vendored set came from",
		Long: `Identify which upstream each vendored set came from.

For every set it fetches candidate npm packages, scores every directory in them
against the committed icons, and reports what it found. Nothing is accepted on
the strength of a name: a candidate is only believed when the bytes it ships
match the bytes already committed.

  exact    every committed icon reproduced byte for byte
  strong   at least 80% reproduced; the right upstream, slightly moved on
  package  at least 95% present by name but no byte matches -- upstream
           identified, version drifted; pin a version
  partial  some names line up -- may be a different project entirely
  none     nothing matched

Writes a manifest to stdout with --json, which is the input a per-set refresh
needs.

For a set the name patterns cannot reach, supply the package yourself and let
the tool answer whether its bytes are the ones already committed:

  imt bundled audit --set phosphor-icons --package @phosphor-icons/core`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			pack, err := config.LoadPack("icon-sets-bundled", configDir())
			if err != nil {
				return failure(err)
			}

			filesRoot := filepath.Join(pack.PackRoot, "resources", "assets", "svg", "files")
			sets, err := bundled.ListSets(filesRoot)
			if err != nil {
				return failure(err)
			}
			if only != "" {
				sets = []string{only}
			}
			if pkg != "" && only == "" {
				return usageErr("--package needs --set: a package can only be verified against one set")
			}

			if workDir == "" {
				workDir, err = os.MkdirTemp("", "imt-bundled-audit-")
				if err != nil {
					return failure(err)
				}
				defer os.RemoveAll(workDir)
			}

			if parallel <= 0 {
				parallel = runtime.NumCPU()
			}

			ui.Info("auditing %d sets from %s", len(sets), filesRoot)
			results := resolveAll(cmd, sets, filesRoot, workDir, parallel, pkg)
			bundled.SortResolutions(results)

			if ui.JSONMode() {
				return ui.JSON(results)
			}

			ui.Printf("%s", renderAudit(results))
			summary := bundled.Summary(results)
			ui.Info("%d exact, %d strong, %d upstream-identified, %d partial, %d unresolved",
				summary[bundled.ConfidenceExact], summary[bundled.ConfidenceStrong],
				summary[bundled.ConfidencePackage], summary[bundled.ConfidencePartial],
				summary[bundled.ConfidenceNone])

			// Deliberately exits 0 even with unresolved sets. This is a survey,
			// not a gate: the unresolved ones are work for a person, and a
			// non-zero exit would make it look like the audit itself failed.
			return nil
		},
	}

	f := cmd.Flags()
	f.StringVar(&only, "set", "", "audit one set")
	f.StringVar(&pkg, "package", "", "verify this npm package against --set, instead of guessing")
	f.StringVar(&workDir, "work-dir", "", "keep downloads here instead of a temporary directory")
	f.IntVar(&parallel, "parallel", 0, "concurrent sets (default: CPU count)")
	return cmd
}

// resolveAll audits sets concurrently.
//
// Each set is independent and dominated by a network fetch, so this is
// worthwhile -- 72 sets fetched one at a time is minutes of waiting. Results
// are written into a fixed slot per set rather than appended, so the output
// order stays deterministic regardless of which finishes first.
func resolveAll(cmd *cobra.Command, sets []string, filesRoot, workDir string, parallel int, pkg string) []bundled.Resolution {
	var explicit []string
	if pkg != "" {
		explicit = []string{pkg}
	}
	results := make([]bundled.Resolution, len(sets))
	fetcher := bundled.NpmFetcher{}

	var wg sync.WaitGroup
	slots := make(chan struct{}, parallel)

	for i, set := range sets {
		wg.Add(1)
		go func(i int, set string) {
			defer wg.Done()
			slots <- struct{}{}
			defer func() { <-slots }()

			results[i] = bundled.ResolveSet(
				cmd.Context(), filepath.Join(filesRoot, set), fetcher, workDir, nil, explicit...)
		}(i, set)
	}
	wg.Wait()
	return results
}

func renderAudit(results []bundled.Resolution) string {
	rows := make([][]string, 0, len(results))
	for _, r := range results {
		var badge string
		switch r.Confidence {
		case bundled.ConfidenceExact:
			badge = ui.OK(ui.Sym.Check + " exact")
		case bundled.ConfidenceStrong:
			badge = ui.OK(ui.Sym.Check + " strong")
		case bundled.ConfidencePackage:
			badge = ui.Warn(ui.Sym.Info + " package")
		case bundled.ConfidencePartial:
			badge = ui.Warn(ui.Sym.Warn + " partial")
		default:
			badge = ui.Err(ui.Sym.Cross + " none")
		}

		pkg := r.Package
		if pkg != "" && r.Version != "" {
			pkg += "@" + r.Version
		}
		if pkg == "" {
			pkg = "-"
		}
		path := r.Path
		if path == "" || path == "." {
			path = "-"
		}

		rows = append(rows, []string{
			r.Set,
			itoa(r.Icons),
			badge,
			itoa(r.Identical) + "/" + itoa(r.Icons),
			pkg,
			path,
		})
	}
	return ui.Table([]string{"Set", "Icons", "Confidence", "Identical", "Package", "Path"}, rows)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
