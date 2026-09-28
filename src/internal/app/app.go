// Package app holds the orchestration both front-ends call.
//
// It exists because the CLI and the TUI must not be two implementations. The
// first attempt put these helpers in internal/cli and had the TUI import them,
// which produced an import cycle the moment the CLI needed to launch the TUI --
// a useful accident, because it forced the shared layer to be named rather
// than left implicit in whichever package happened to define it first.
package app

import (
	"context"

	"github.com/ichava/maintainer-toolkit/src/internal/core/checker"
	"github.com/ichava/maintainer-toolkit/src/internal/core/config"
)

// LoadPacks loads one pack, or every pack in registry order.
//
// Shared so the two front-ends cannot disagree about what "every pack" means.
func LoadPacks(configDir, only string) ([]*config.PackConfig, error) {
	if only != "" {
		pack, err := config.LoadPack(only, configDir)
		if err != nil {
			return nil, err
		}
		return []*config.PackConfig{pack}, nil
	}
	return config.LoadAll(configDir)
}

// CheckAll resolves upstream status for every pack, in order.
//
// Sequential on purpose. Polling four registries concurrently would save a
// second or two and cost the deterministic output order that makes a CI log
// diffable between runs.
func CheckAll(ctx context.Context, packs []*config.PackConfig) []checker.Result {
	c := checker.New()
	results := make([]checker.Result, 0, len(packs))
	for _, p := range packs {
		results = append(results, c.CheckPack(ctx, p))
	}
	return results
}
