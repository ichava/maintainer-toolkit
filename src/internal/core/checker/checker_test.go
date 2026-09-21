package checker

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ichava/maintainer-toolkit/src/internal/core/config"
)

// The ParseLatest and ResolveReleaseURL cases below are ports of
// tests/unit/test_checker.py, which itself mirrors the PHP
// IconPackUpdateCheckerTest cases.

func payload(t *testing.T, body string) any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(body), &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestParseLatest(t *testing.T) {
	for _, tc := range []struct {
		name   string
		source config.SourceConfig
		body   string
		want   string
	}{
		{"npm", config.SourceConfig{Type: "npm"}, `{"version":"5.4.2"}`, "5.4.2"},
		{"github-release strips the v", config.SourceConfig{Type: "github-release"},
			`{"tag_name":"v17.1.0","html_url":"https://example.test/r"}`, "17.1.0"},
		{"github-tag takes the first entry", config.SourceConfig{Type: "github-tag"},
			`[{"name":"v7.4.0"},{"name":"v7.3.9"}]`, "7.4.0"},
		{"github-tag falls back to ref", config.SourceConfig{Type: "github-tag"},
			`[{"ref":"refs-v2.1.0"}]`, "refs-v2.1.0"},
		{"url walks a dot path", config.SourceConfig{
			Type: "url", Extra: map[string]any{"version_field": "dist-tags.latest"}},
			`{"dist-tags":{"latest":"9.9.9"}}`, "9.9.9"},
		{"url defaults to version", config.SourceConfig{Type: "url"},
			`{"version":"2.2.0"}`, "2.2.0"},

		// Shapes with no answer. Each reports "could not parse latest" rather
		// than erroring, so one odd pack does not abort a multi-pack run.
		{"url dot path missing", config.SourceConfig{
			Type: "url", Extra: map[string]any{"version_field": "a.b"}}, `{"a":{}}`, ""},
		{"url value is not a string", config.SourceConfig{Type: "url"}, `{"version":7}`, ""},
		{"github-tag empty list", config.SourceConfig{Type: "github-tag"}, `[]`, ""},
		{"npm given a list", config.SourceConfig{Type: "npm"}, `[]`, ""},
		// Neither declares a pollable endpoint, so neither has a latest.
		{"github-archive has none", config.SourceConfig{Type: "github-archive"}, `{"version":"1"}`, ""},
		{"script has none", config.SourceConfig{Type: "script"}, `{"version":"1"}`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ParseLatest(tc.source, payload(t, tc.body)); got != tc.want {
				t.Errorf("ParseLatest = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestResolveReleaseURL(t *testing.T) {
	for _, tc := range []struct {
		name    string
		source  config.SourceConfig
		body    string
		version string
		want    string
	}{
		{"npm synthesises", config.SourceConfig{Type: "npm", Package: "@twemoji/svg"},
			`{}`, "17.1.0", "https://www.npmjs.com/package/@twemoji/svg/v/17.1.0"},
		{"npm without a version", config.SourceConfig{Type: "npm", Package: "flag-icons"},
			`{}`, "", "https://www.npmjs.com/package/flag-icons"},
		{"github-tag synthesises", config.SourceConfig{Type: "github-tag", Owner: "lipis", Repo: "flag-icons"},
			`[]`, "7.4.0", "https://github.com/lipis/flag-icons/releases/tag/v7.4.0"},
		// A project can publish a release whose page is not at the
		// conventional tag URL, so its own html_url wins.
		{"github-release prefers html_url", config.SourceConfig{
			Type: "github-release", Owner: "o", Repo: "r"},
			`{"html_url":"https://example.test/actual","tag_name":"v1.0.0"}`, "1.0.0",
			"https://example.test/actual"},
		{"github-release synthesises without html_url", config.SourceConfig{
			Type: "github-release", Owner: "o", Repo: "r"}, `{}`, "1.0.0",
			"https://github.com/o/r/releases/tag/v1.0.0"},
		{"url has no release page", config.SourceConfig{Type: "url"}, `{}`, "1.0.0", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ResolveReleaseURL(tc.source, payload(t, tc.body), tc.version); got != tc.want {
				t.Errorf("ResolveReleaseURL = %q, want %q", got, tc.want)
			}
		})
	}
}

// stubFetcher answers with a fixed payload or a fixed error, so the checker can
// be exercised without a registry.
type stubFetcher struct {
	body string
	err  error
}

func (s stubFetcher) GetJSON(_ context.Context, _ string, out any) error {
	if s.err != nil {
		return s.err
	}
	return json.Unmarshal([]byte(s.body), out)
}

// packWithVendored builds a pack repo recording a vendored version.
func packWithVendored(t *testing.T, vendored string) *config.PackConfig {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "resources", "assets", "svg")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"package":{"version":"0.1.0","upstream_version":"` + vendored +
		`"},"upstream":{"current_version":"` + vendored + `"}}`
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return &config.PackConfig{
		Name: "demo", Pack: "ichava/icon-sets-demo", PackRoot: root,
		CurrentVersion:  "3.0.0", // the stale toolkit-side fallback
		VersionFile:     config.DefaultVersionFile,
		VersionKeys:     config.DefaultVersionKeys(),
		VersionCheckURL: "https://registry.example/demo",
		Source:          config.SourceConfig{Type: "npm"},
	}
}

// TestTheSyncConverges is the regression the whole vendored-version design
// exists for. The pack already ships 3.46.0; this repository's config still
// says 3.0.0. Comparing against the config would re-propose the same refresh
// forever, because the bump is written into the runner's throwaway checkout.
func TestTheSyncConverges(t *testing.T) {
	c := &Checker{Fetch: stubFetcher{body: `{"version":"3.46.0"}`}}

	got := c.CheckPack(context.Background(), packWithVendored(t, "3.46.0"))

	if got.Current != "3.46.0" {
		t.Errorf("Current = %q, want 3.46.0 -- the pack repo must win", got.Current)
	}
	if got.Stale {
		t.Error("a pack already vendoring the latest version is not stale")
	}
	if got.Status() != StatusOK {
		t.Errorf("Status = %q", got.Status())
	}
}

func TestStillDetectsAGenuineUpstreamBump(t *testing.T) {
	c := &Checker{Fetch: stubFetcher{body: `{"version":"3.47.0"}`}}

	got := c.CheckPack(context.Background(), packWithVendored(t, "3.46.0"))

	if !got.Stale {
		t.Error("3.46.0 vendored against 3.47.0 upstream is stale")
	}
	if got.Latest != "3.47.0" {
		t.Errorf("Latest = %q", got.Latest)
	}
	if got.Status() != StatusUpdateAvailable {
		t.Errorf("Status = %q", got.Status())
	}
}

func TestAPackWithoutACheckURLStillReportsTheVendoredVersion(t *testing.T) {
	pack := packWithVendored(t, "3.46.0")
	pack.VersionCheckURL = ""

	got := (&Checker{Fetch: stubFetcher{}}).CheckPack(context.Background(), pack)

	if got.Current != "3.46.0" {
		t.Errorf("Current = %q, want the vendored version", got.Current)
	}
	if got.Reason != "no version_check_url" {
		t.Errorf("Reason = %q", got.Reason)
	}
	if got.Stale {
		t.Error("a pack that cannot be checked must not be reported stale")
	}
}

// TestAnUnreachableRegistryIsReportedNotRaised keeps one dead endpoint from
// aborting a `check --all` across every other pack.
func TestAnUnreachableRegistryIsReportedNotRaised(t *testing.T) {
	c := &Checker{Fetch: stubFetcher{err: errors.New("connection refused")}}

	got := c.CheckPack(context.Background(), packWithVendored(t, "3.46.0"))

	if got.Reason == "" || got.Status() != StatusError {
		t.Fatalf("Reason = %q, Status = %q", got.Reason, got.Status())
	}
	if got.Stale {
		t.Error("an unreachable registry tells us nothing about staleness")
	}
	if got.Current != "3.46.0" {
		t.Errorf("Current = %q -- the vendored version is still known", got.Current)
	}
}

func TestAnUnparseablePayloadIsReported(t *testing.T) {
	c := &Checker{Fetch: stubFetcher{body: `{"nothing":"useful"}`}}

	got := c.CheckPack(context.Background(), packWithVendored(t, "3.46.0"))

	if got.Reason != "could not parse latest" {
		t.Errorf("Reason = %q", got.Reason)
	}
}

func TestAnyStaleAndCountStale(t *testing.T) {
	results := []Result{
		{Latest: "1.0.0", Stale: false},
		{Latest: "2.0.0", Stale: true},
		{Reason: "unreachable: x", Stale: true}, // no Latest: not counted
		{Latest: "3.0.0", Stale: true},
	}
	if !AnyStale(results) {
		t.Error("AnyStale = false")
	}
	if got := CountStale(results); got != 2 {
		t.Errorf("CountStale = %d, want 2 -- a result with no Latest is not a known bump", got)
	}
	if AnyStale(results[:1]) {
		t.Error("AnyStale on an up-to-date set")
	}
}

func TestDetailPrefersTheReasonOverTheLink(t *testing.T) {
	if got := (Result{Reason: "unreachable: x", ReleaseURL: "https://y"}).Detail(); got != "unreachable: x" {
		t.Errorf("Detail = %q", got)
	}
	if got := (Result{ReleaseURL: "https://y"}).Detail(); got != "-> https://y" {
		t.Errorf("Detail = %q", got)
	}
	if got := (Result{}).Detail(); got != "-" {
		t.Errorf("Detail = %q", got)
	}
}
