// Package cli is the cobra command tree. It stays thin: every command parses
// flags and calls into internal/core, so the TUI can call the same functions
// and the two front-ends cannot drift into two implementations.
package cli

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	"github.com/ichava/maintainer-toolkit/src/internal/core/config"
	"github.com/ichava/maintainer-toolkit/src/internal/ui"
	"github.com/ichava/maintainer-toolkit/src/internal/version"
)

// Exit codes. These are a contract, not an implementation detail: `check`
// exiting 1 on a stale pack is what makes it usable as a CI gate, and Click
// used 2 for a usage error, which anything wrapping the Python binary may
// already depend on.
const (
	ExitOK      = 0
	ExitFailure = 1
	ExitUsage   = 2
)

// ExitError carries an exit code out of a command.
type ExitError struct {
	Code int
	Err  error
}

func (e *ExitError) Error() string {
	if e.Err == nil {
		return fmt.Sprintf("exit %d", e.Code)
	}
	return e.Err.Error()
}
func (e *ExitError) Unwrap() error { return e.Err }

func usageErr(format string, a ...any) error {
	return &ExitError{Code: ExitUsage, Err: fmt.Errorf(format, a...)}
}

func failure(err error) error { return &ExitError{Code: ExitFailure, Err: err} }

// globals holds the flags every command shares.
type globals struct {
	configDir string
	verbose   bool
	quiet     bool
	noColor   bool
	noInput   bool
	assumeYes bool
	jsonOut   bool
}

var g globals

// Execute builds the command tree and runs it, returning the process exit code.
func Execute() int {
	root := NewRootCommand()
	if err := root.Execute(); err != nil {
		var exit *ExitError
		if ok := asExitError(err, &exit); ok {
			if exit.Err != nil {
				ui.Failure("%s", exit.Err)
			}
			return exit.Code
		}
		ui.Failure("%s", err)
		return ExitFailure
	}
	return ExitOK
}

func asExitError(err error, target **ExitError) bool {
	for err != nil {
		if e, ok := err.(*ExitError); ok {
			*target = e
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

// NewRootCommand builds the tree.
func NewRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:   version.ShortName,
		Short: "Maintainer toolkit for the Ichava icon-pack ecosystem",
		Long: `Maintainer toolkit for the Ichava icon-pack ecosystem.

Polls upstream icon registries, refreshes vendored SVGs into the pack repos,
stamps the version and opens a pull request.

Run with no arguments in a terminal for the interactive menu.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		// A usage error is exit 2, matching what Click did for the Python.
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMenu(cmd, args)
		},
	}

	pf := root.PersistentFlags()
	pf.StringVar(&g.configDir, "config-dir", "", "where pack configs live (default: $ICHAVA_DEV_CONFIG_DIR, else ./config)")
	pf.BoolVarP(&g.verbose, "verbose", "v", false, "debug logging")
	pf.BoolVarP(&g.quiet, "quiet", "q", false, "suppress status output")
	pf.BoolVar(&g.noColor, "no-color", false, "disable colour (also honours NO_COLOR)")
	pf.BoolVar(&g.noInput, "no-input", false, "never prompt; fail instead of asking")
	pf.BoolVar(&g.assumeYes, "yes", false, "assume yes for confirmations")
	pf.BoolVar(&g.jsonOut, "json", false, "machine-readable output")

	root.PersistentPreRun = func(_ *cobra.Command, _ []string) {
		ui.Init(ui.Config{
			AssumeYes: g.assumeYes,
			NoInput:   g.noInput,
			NoColor:   g.noColor,
			JSON:      g.jsonOut,
			Quiet:     g.quiet,
		})
		setupLogging(g.verbose)
	}

	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return usageErr("%s", err)
	})

	root.AddCommand(
		newListPacksCommand(),
		newCheckCommand(),
		newSyncCommand(),
		newRecipeCommand(),
		newBundledCommand(),
		newMenuCommand(),
		newVersionCommand(),
	)
	return root
}

// setupLogging sends structured logs to stderr, so they never contaminate the
// stdout a caller may be piping into jq.
func setupLogging(verbose bool) {
	level := slog.LevelInfo
	if verbose {
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))
}

// configDir resolves the directory to read pack configs from.
func configDir() string {
	if g.configDir != "" {
		return g.configDir
	}
	return config.DefaultConfigDir()
}

func newVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version and exit",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// The Docker smoke test calls this, and it compares the whole
			// line, so the "<name> <version>" shape is load-bearing.
			cmd.Printf("%s %s\n", version.Name, version.Version())
			if g.verbose {
				if c := version.Commit(); c != "" {
					cmd.Printf("commit %s\n", c)
				}
				if d := version.Date(); d != "" {
					cmd.Printf("built  %s\n", d)
				}
			}
			return nil
		},
	}
}
