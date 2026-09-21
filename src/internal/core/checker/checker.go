// Package checker resolves the latest upstream version for a pack and compares
// it against what the pack repo vendors.
//
// It is one half of a two-implementation contract: `ichava/core`'s
// IconPackUpdateChecker powers host-app discovery through
// `php artisan ichava::ichava-core.check-updates`, and this drives the
// maintainer-side refresh. Where the two disagreed, see version.go.
package checker

import (
	"context"
	"fmt"
	"strings"

	"github.com/ichava/maintainer-toolkit/src/internal/core/config"
	"github.com/ichava/maintainer-toolkit/src/internal/core/httpx"
)

// Result is one pack's upstream status.
type Result struct {
	Package    string
	Current    string
	Latest     string
	Stale      bool
	ReleaseURL string

	// Reason is set when Latest could not be established. It is not a failure:
	// an unreachable registry leaves the pack unchanged rather than aborting a
	// multi-pack run.
	Reason string
}

// Fetcher is the HTTP surface the checker needs, narrowed so a test can supply
// a payload without a network or a live registry.
type Fetcher interface {
	GetJSON(ctx context.Context, url string, out any) error
}

// Checker resolves upstream versions.
type Checker struct{ Fetch Fetcher }

// New returns a checker over the default HTTP client.
func New() *Checker { return &Checker{Fetch: httpx.New()} }

// CheckPack resolves the latest version and compares it against the pack.
//
// The comparison base is config.ResolvedCurrentVersion, which reads the pack
// repo's own record first. Comparing against this repository's current_version
// is what made the sync non-convergent (V49): the bump was written into the
// runner's throwaway checkout and never committed, so every run saw the same
// stale value and re-proposed the same refresh.
func (c *Checker) CheckPack(ctx context.Context, pack *config.PackConfig) Result {
	current := config.ResolvedCurrentVersion(pack)

	if pack.VersionCheckURL == "" {
		return Result{Package: pack.Pack, Current: current, Reason: "no version_check_url"}
	}

	var payload any
	if err := c.Fetch.GetJSON(ctx, pack.VersionCheckURL, &payload); err != nil {
		return Result{Package: pack.Pack, Current: current, Reason: "unreachable: " + err.Error()}
	}

	latest := ParseLatest(pack.Source, payload)
	if latest == "" {
		return Result{Package: pack.Pack, Current: current, Reason: "could not parse latest"}
	}

	return Result{
		Package:    pack.Pack,
		Current:    current,
		Latest:     latest,
		Stale:      IsStale(current, latest),
		ReleaseURL: ResolveReleaseURL(pack.Source, payload, latest),
	}
}

// ParseLatest extracts the latest version from a registry payload.
//
// Returns "" for a shape it does not recognise, which the caller reports as
// "could not parse latest" rather than treating as an error. Note that
// github-archive and script sources fall through here: neither declares a
// pollable endpoint, so neither has a latest version to find.
func ParseLatest(source config.SourceConfig, payload any) string {
	switch source.Type {
	case "npm":
		if obj, ok := payload.(map[string]any); ok {
			if v, ok := obj["version"].(string); ok {
				return TrimVersion(v)
			}
		}

	case "github-tag":
		// Tags come back newest first, so only the head entry matters.
		if list, ok := payload.([]any); ok && len(list) > 0 {
			if first, ok := list[0].(map[string]any); ok {
				if v, ok := first["name"].(string); ok && v != "" {
					return TrimVersion(v)
				}
				if v, ok := first["ref"].(string); ok && v != "" {
					return TrimVersion(v)
				}
			}
		}

	case "github-release":
		if obj, ok := payload.(map[string]any); ok {
			if v, ok := obj["tag_name"].(string); ok {
				return TrimVersion(v)
			}
		}

	case "url":
		if obj, ok := payload.(map[string]any); ok {
			if v, ok := walkDotPath(obj, source.VersionField()); ok {
				return TrimVersion(v)
			}
		}
	}

	return ""
}

// walkDotPath resolves a dotted path to a string value.
func walkDotPath(payload map[string]any, path string) (string, bool) {
	var cursor any = payload
	for _, seg := range strings.Split(path, ".") {
		obj, ok := cursor.(map[string]any)
		if !ok {
			return "", false
		}
		cursor, ok = obj[seg]
		if !ok {
			return "", false
		}
	}
	s, ok := cursor.(string)
	return s, ok
}

// ResolveReleaseURL synthesises a clickable release link.
//
// The order is checked, not incidental: a github-release payload's own
// html_url is authoritative and wins over anything synthesised from owner and
// repo, because a project can publish a release whose page is not at the
// conventional tag URL.
func ResolveReleaseURL(source config.SourceConfig, payload any, version string) string {
	if source.Type == "github-release" {
		if obj, ok := payload.(map[string]any); ok {
			if u, ok := obj["html_url"].(string); ok && u != "" {
				return u
			}
		}
	}

	if (source.Type == "github-release" || source.Type == "github-tag") &&
		source.Owner != "" && source.Repo != "" {
		if version == "" {
			return fmt.Sprintf("https://github.com/%s/%s", source.Owner, source.Repo)
		}
		return fmt.Sprintf("https://github.com/%s/%s/releases/tag/v%s", source.Owner, source.Repo, version)
	}

	if source.Type == "npm" && source.Package != "" {
		if version == "" {
			return "https://www.npmjs.com/package/" + source.Package
		}
		return fmt.Sprintf("https://www.npmjs.com/package/%s/v/%s", source.Package, version)
	}

	return ""
}

// Status is the one-word verdict the reporter renders.
type Status string

const (
	StatusOK              Status = "ok"
	StatusUpdateAvailable Status = "update-available"
	StatusError           Status = "error"
)

// Status classifies a result the way the TTY reporter does.
func (r Result) Status() Status {
	switch {
	case r.Latest != "" && !r.Stale:
		return StatusOK
	case r.Latest != "" && r.Stale:
		return StatusUpdateAvailable
	default:
		return StatusError
	}
}

// Detail is the rightmost column: the reason a check could not complete, or
// the release link when it did.
func (r Result) Detail() string {
	if r.Reason != "" {
		return r.Reason
	}
	if r.ReleaseURL != "" {
		return "-> " + r.ReleaseURL
	}
	return "-"
}

// AnyStale reports whether any result is behind upstream. `check` exits 1 on
// this, which is what makes it usable as a CI gate.
func AnyStale(results []Result) bool {
	for _, r := range results {
		if r.Stale && r.Latest != "" {
			return true
		}
	}
	return false
}

// CountErrored returns how many results could not be resolved at all.
//
// This exists because summarising on stale alone produces a false green: when
// every registry is unreachable nothing is *known* to be stale, so a naive
// "0 stale, therefore all up to date" reports success for a run that checked
// nothing. The Python reporter had exactly that bug, and it is the shape of
// failure this estate keeps paying for -- a green that means "did not look".
func CountErrored(results []Result) int {
	n := 0
	for _, r := range results {
		if r.Latest == "" {
			n++
		}
	}
	return n
}

// CountStale returns how many results are behind upstream.
func CountStale(results []Result) int {
	n := 0
	for _, r := range results {
		if r.Stale && r.Latest != "" {
			n++
		}
	}
	return n
}
