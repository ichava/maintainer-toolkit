// Command imt is the Ichava maintainer toolkit.
//
// Thin by design: it builds the cobra root and exits with whatever code the
// command returned. Everything else lives under internal/, so the CLI and the
// TUI stay two front-ends over one engine rather than two implementations.
//
// The binary ships under two names -- `ichava-maintainer-toolkit` for scripts
// and docs, `imt` for interactive use -- matching the two console scripts the
// Python declared.
package main

import (
	"os"

	"github.com/ichava/maintainer-toolkit/src/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
