package cli

import (
	"github.com/spf13/cobra"

	"github.com/ichava/maintainer-toolkit/src/internal/tui"
	"github.com/ichava/maintainer-toolkit/src/internal/ui"
)

func newMenuCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "menu",
		Short: "Interactive menu (the default action in a terminal)",
		Args:  cobra.NoArgs,
		RunE:  runMenu,
	}
}

// runMenu launches the TUI, or prints help when there is nobody to drive it.
//
// This is the Docker image's default CMD, and the image is run by a scheduled
// GitHub Actions job as often as by a person. The Python version called
// questionary.select() unconditionally, which on a runner blocks on a stdin
// that never answers -- a job that hangs until the 30-minute timeout rather
// than one that says what it wanted.
func runMenu(cmd *cobra.Command, _ []string) error {
	if !ui.Interactive() {
		ui.Info("not a terminal, so there is nothing to drive the menu; printing help instead.")
		return cmd.Help()
	}
	if err := tui.Run(configDir()); err != nil {
		return failure(err)
	}
	return nil
}
