package recipes

import (
	"strings"
	"testing"

	"github.com/ichava/maintainer-toolkit/src/internal/core/bundled"
	"github.com/ichava/maintainer-toolkit/src/internal/core/config"
	"github.com/ichava/maintainer-toolkit/src/internal/core/pipeline"
	"github.com/ichava/maintainer-toolkit/src/internal/core/transforms"
)

func npmPack(name string) *config.PackConfig {
	return &config.PackConfig{
		Name: name, Pack: "ichava/" + name, PackRoot: "/work/" + name,
		CurrentVersion: "1.0.0",
		VersionFile:    config.DefaultVersionFile,
		VersionKeys:    config.DefaultVersionKeys(),
		Source:         config.SourceConfig{Type: "npm", Package: "@x/y", SourcePath: "icons"},
		Sinks: []config.SinkConfig{
			{Type: "filesystem", Root: "{pack_root}/resources/assets/svg/files"},
			{Type: "git-branch", RepoRoot: "{pack_root}", Branch: "chore/sync-upstream"},
		},
	}
}

func stageNames(p *pipeline.Pipeline) []string {
	var names []string
	for _, s := range p.Stages() {
		names = append(names, s.Name())
	}
	return names
}

func TestSimpleNPMComposesTheExpectedStages(t *testing.T) {
	built, err := buildFor(npmPack("icon-sets-tabler"), "3.47.0", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(built) != 1 {
		t.Fatalf("got %d pipelines, want 1", len(built))
	}

	got := strings.Join(stageNames(built[0]), ",")
	want := "NpmTarball,SubsetTo,Sanitise,Filesystem,VersionStamp,GitBranch"
	if got != want {
		t.Errorf("stages = %s\nwant    %s", got, want)
	}
}

// TestVersionStampPrecedesGitBranch is the V49 ordering. Stamping after the
// commit records a version that never shipped, and the next run then sees the
// old one and re-proposes the same refresh forever.
func TestVersionStampPrecedesGitBranch(t *testing.T) {
	built, _ := buildFor(npmPack("icon-sets-flag"), "7.5.0", false)
	names := stageNames(built[0])

	stamp, commit := indexOf(names, "VersionStamp"), indexOf(names, "GitBranch")
	if stamp < 0 || commit < 0 {
		t.Fatalf("stages = %v", names)
	}
	if stamp > commit {
		t.Errorf("VersionStamp runs after GitBranch, so the commit records the previous version: %v", names)
	}
}

func TestDryRunDropsTheCommittingSinks(t *testing.T) {
	built, err := buildFor(npmPack("icon-sets-flag"), "7.5.0", true)
	if err != nil {
		t.Fatal(err)
	}
	names := stageNames(built[0])

	for _, unwanted := range []string{"GitBranch", "VersionStamp"} {
		if indexOf(names, unwanted) >= 0 {
			t.Errorf("a dry run must not include %s: %v", unwanted, names)
		}
	}
	if indexOf(names, "Filesystem") < 0 {
		t.Errorf("a dry run should still write the assets locally: %v", names)
	}
}

// TestDispatchIsByNameBeforeSourceType is the trap the Python comment names.
// icon-sets-emoji declares source.type "url" because that is how its version
// is discovered; its assets come from three upstreams only the named recipe
// knows about. Checking the type first sends it to "no recipe wired".
func TestDispatchIsByNameBeforeSourceType(t *testing.T) {
	emoji := &config.PackConfig{
		Name: "icon-sets-emoji", Pack: "ichava/icon-sets-emoji", PackRoot: "/work/icon-sets-emoji",
		VersionFile: config.DefaultVersionFile, VersionKeys: config.DefaultVersionKeys(),
		Source: config.SourceConfig{Type: "url"},
	}

	built, err := buildFor(emoji, "15.0.0", false)
	if err != nil {
		t.Fatalf("a url-typed emoji pack must still reach its recipe: %v", err)
	}
	if len(built) != 4 {
		t.Fatalf("got %d pipelines, want 4 (twemoji, two openmoji, commit)", len(built))
	}
}

// TestEmojiCommitPipelineCarriesAVersion is the bug this port fixes. Python's
// commit pipeline used a source that set nothing, so VersionStamp found no
// fetched_version and silently skipped -- icon-sets-emoji was never stamped
// and its sync could not converge.
func TestEmojiCommitPipelineCarriesAVersion(t *testing.T) {
	emoji := &config.PackConfig{
		Name: "icon-sets-emoji", Pack: "ichava/icon-sets-emoji", PackRoot: "/work/icon-sets-emoji",
		VersionFile: config.DefaultVersionFile, VersionKeys: config.DefaultVersionKeys(),
		Source: config.SourceConfig{Type: "url"},
	}

	built, _ := buildFor(emoji, "15.0.0", false)
	commit := built[len(built)-1]

	if commit.Name() != "emoji-sets:commit" {
		t.Fatalf("last pipeline is %q", commit.Name())
	}
	names := stageNames(commit)
	if indexOf(names, "StaticSource") < 0 {
		t.Errorf("the commit pipeline needs a source that records the version: %v", names)
	}
	if indexOf(names, "VersionStamp") < 0 {
		t.Errorf("the commit pipeline must stamp: %v", names)
	}
}

func TestEmojiDryRunHasNoCommitPipeline(t *testing.T) {
	emoji := &config.PackConfig{
		Name: "icon-sets-emoji", Pack: "ichava/icon-sets-emoji", PackRoot: "/work/icon-sets-emoji",
		VersionFile: config.DefaultVersionFile, VersionKeys: config.DefaultVersionKeys(),
		Source: config.SourceConfig{Type: "url"},
	}
	built, _ := buildFor(emoji, "15.0.0", true)
	if len(built) != 3 {
		t.Fatalf("got %d pipelines, want 3 asset pipelines and no commit", len(built))
	}
}

// TestBundledNeedsAManifestAndSaysSo replaces the old recipe-pending test.
// icon-sets-bundled now has a real recipe, so the failure mode changed from
// "not written" to "not configured" -- and the error has to name the command
// that builds the manifest, because that is the whole remaining task.
func TestBundledNeedsAManifestAndSaysSo(t *testing.T) {
	bundled := &config.PackConfig{
		Name: "icon-sets-bundled", Pack: "ichava/icon-sets-bundled",
		Source: config.SourceConfig{Type: "url"},
	}

	_, err := Build(bundled, "2.2.0", false, t.TempDir())
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "missing set manifest") {
		t.Errorf("err = %q, want it to name the missing manifest", err)
	}
	if !strings.Contains(err.Error(), "bundled audit") {
		t.Errorf("err = %q, want it to name the command that builds one", err)
	}
}

// TestBundledBuildsOnePipelinePerSet covers the shape, and the rule that
// matters most: a set absent from the manifest is not touched at all. The
// filesystem sink wipes its destination, so a guessed entry would replace a
// set's icons with another project's -- absent is safe, wrong is not.
func TestBundledBuildsOnePipelinePerSet(t *testing.T) {
	dir := t.TempDir()
	m := &bundled.Manifest{
		Pack: "ichava/icon-sets-bundled",
		Sets: map[string]bundled.SetSource{
			"bootstrap-icons": {Package: "bootstrap-icons", Path: "package/icons", Confidence: bundled.ConfidenceExact},
			"feather-icons":   {Package: "feather-icons", Path: "package/dist/icons", Version: "4.29.2"},
		},
	}
	if err := m.Save(bundled.ManifestPath(dir, "icon-sets-bundled")); err != nil {
		t.Fatal(err)
	}

	pack := &config.PackConfig{
		Name: "icon-sets-bundled", Pack: "ichava/icon-sets-bundled", PackRoot: "/work/icon-sets-bundled",
		VersionFile: config.DefaultVersionFile, VersionKeys: config.DefaultVersionKeys(),
		Source: config.SourceConfig{Type: "url"},
	}

	built, err := Build(pack, "2.2.0", false, dir)
	if err != nil {
		t.Fatal(err)
	}
	// Two sets plus the single commit pipeline.
	if len(built) != 3 {
		t.Fatalf("got %d pipelines, want 3", len(built))
	}
	if got := built[len(built)-1].Name(); got != "icon-sets-bundled:commit" {
		t.Errorf("last pipeline = %q", got)
	}

	// Sorted by set name, so two runs produce comparable logs.
	if !strings.Contains(built[0].Name(), "bootstrap-icons") {
		t.Errorf("first pipeline = %q, want the alphabetically first set", built[0].Name())
	}
	// An unpinned set tracks latest; a pinned one does not.
	if !strings.HasSuffix(built[0].Name(), "@latest") {
		t.Errorf("unpinned set should track latest: %q", built[0].Name())
	}
	if !strings.HasSuffix(built[1].Name(), "@4.29.2") {
		t.Errorf("pinned set should use its pin: %q", built[1].Name())
	}

	names := stageNames(built[0])
	want := "NpmTarball,SubsetTo,Sanitise,Filesystem"
	if strings.Join(names, ",") != want {
		t.Errorf("stages = %v, want %s", names, want)
	}
}

// TestBundledDoesNotEnforceThePolicy is a deliberate difference from every
// other pack. This one has never been sanitised: its icons keep comments the
// policy strips, and enforcing during a refresh would rewrite tens of
// thousands of files and drop fontawesome's licence attribution.
func TestBundledDoesNotEnforceThePolicy(t *testing.T) {
	dir := t.TempDir()
	m := &bundled.Manifest{Sets: map[string]bundled.SetSource{
		"x": {Package: "x", Path: "package"},
	}}
	if err := m.Save(bundled.ManifestPath(dir, "icon-sets-bundled")); err != nil {
		t.Fatal(err)
	}
	pack := &config.PackConfig{
		Name: "icon-sets-bundled", Pack: "ichava/icon-sets-bundled", PackRoot: "/work/b",
		VersionFile: config.DefaultVersionFile, VersionKeys: config.DefaultVersionKeys(),
		Source: config.SourceConfig{Type: "url"},
	}

	built, err := Build(pack, "2.2.0", true, dir)
	if err != nil {
		t.Fatal(err)
	}

	for _, stage := range built[0].Stages() {
		if s, ok := stage.(transforms.Sanitise); ok {
			if !s.ReportOnly {
				t.Error("the bundled refresh must report policy violations, not rewrite the files")
			}
			return
		}
	}
	t.Error("no Sanitise stage: the policy should still be evaluated, just not enforced")
}

// TestBundledRefusesAManifestForAnotherPack guards against a copied file
// pointing a wiping refresh at the wrong tree.
func TestBundledRefusesAManifestForAnotherPack(t *testing.T) {
	dir := t.TempDir()
	m := &bundled.Manifest{Pack: "ichava/icon-sets-tabler", Sets: map[string]bundled.SetSource{
		"x": {Package: "x", Path: "package"},
	}}
	if err := m.Save(bundled.ManifestPath(dir, "icon-sets-bundled")); err != nil {
		t.Fatal(err)
	}
	pack := &config.PackConfig{
		Name: "icon-sets-bundled", Pack: "ichava/icon-sets-bundled",
		Source: config.SourceConfig{Type: "url"},
	}

	if _, err := Build(pack, "2.2.0", false, dir); err == nil ||
		!strings.Contains(err.Error(), "names") {
		t.Fatalf("err = %v, want it to refuse a manifest for another pack", err)
	}
}

func TestAnUnwiredSourceTypeIsNamed(t *testing.T) {
	pack := &config.PackConfig{
		Name: "icon-sets-new", Pack: "ichava/icon-sets-new",
		Source: config.SourceConfig{Type: "script"},
	}
	_, err := buildFor(pack, "1.0.0", false)
	if err == nil || !strings.Contains(err.Error(), `source.type="script"`) {
		t.Fatalf("err = %v, want it to name the unwired type", err)
	}
}

func TestSimpleNPMRequiresAPackage(t *testing.T) {
	pack := npmPack("icon-sets-x")
	pack.Source.Package = ""
	if _, err := buildFor(pack, "1.0.0", false); err == nil {
		t.Fatal("expected an error naming the missing source.package")
	}
}

// TestOldVersionPrefersWhatThePackShips keeps the commit message honest: this
// repository's current_version is only a fallback, and using it would name a
// version the pack left behind long ago.
func TestOldVersionPrefersWhatThePackShips(t *testing.T) {
	pack := npmPack("icon-sets-flag")
	pack.PackRoot = t.TempDir()

	cfg := packConfig(pack)
	if got := cfg[pipeline.ConfigOldVersion]; got != "1.0.0" {
		t.Errorf("with no vendored record, old_version = %v, want the config fallback", got)
	}

	pack.PackRoot = "/definitely/not/here"
	pack.CurrentVersion = ""
	cfg = packConfig(pack)
	if got := cfg[pipeline.ConfigOldVersion]; got != "unknown" {
		t.Errorf("with neither, old_version = %v, want \"unknown\"", got)
	}
}

func indexOf(items []string, want string) int {
	for i, s := range items {
		if s == want {
			return i
		}
	}
	return -1
}

// buildFor keeps the tests readable now that Build takes a config directory;
// only the bundled recipe uses it.
func buildFor(pack *config.PackConfig, version string, dryRun bool) ([]*pipeline.Pipeline, error) {
	return Build(pack, version, dryRun, "")
}
