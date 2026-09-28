package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writePack builds a pack repo with a version record, matching the shape
// tests/unit/test_vendored_version.py builds.
func writePack(t *testing.T, version string) *PackConfig {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "resources", "assets", "svg")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{
  "package": {
    "name": "ichava/icon-sets-demo",
    "version": "0.1.0",
    "upstream_version": "` + version + `"
  },
  "upstream": {
    "current_version": "` + version + `"
  }
}
`
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return &PackConfig{
		Name: "demo", Pack: "ichava/icon-sets-demo", PackRoot: root,
		CurrentVersion: "3.0.0",
		VersionFile:    DefaultVersionFile, VersionKeys: DefaultVersionKeys(),
	}
}

func TestReadsTheVersionThePackRepoRecords(t *testing.T) {
	if got := ReadVendoredVersion(writePack(t, "3.46.0")); got != "3.46.0" {
		t.Fatalf("got %q, want 3.46.0", got)
	}
}

func TestThePackRepoWinsOverTheToolkitConfig(t *testing.T) {
	pack := writePack(t, "3.46.0") // toolkit config says 3.0.0
	if got := ResolvedCurrentVersion(pack); got != "3.46.0" {
		t.Fatalf("got %q, want 3.46.0 -- the toolkit config must not win", got)
	}
}

func TestFallsBackToTheToolkitConfigWhenThePackHasNoRecord(t *testing.T) {
	pack := &PackConfig{
		PackRoot: t.TempDir(), CurrentVersion: "3.0.0",
		VersionFile: DefaultVersionFile, VersionKeys: DefaultVersionKeys(),
	}
	if got := ReadVendoredVersion(pack); got != "" {
		t.Fatalf("ReadVendoredVersion = %q, want empty", got)
	}
	if got := ResolvedCurrentVersion(pack); got != "3.0.0" {
		t.Fatalf("ResolvedCurrentVersion = %q, want 3.0.0", got)
	}
}

func TestWritingUpdatesEveryDeclaredKeyAndLeavesPackageVersionAlone(t *testing.T) {
	pack := writePack(t, "3.0.0")

	if _, err := WriteVendoredVersion(pack, "3.46.0"); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(VendoredVersionFile(pack))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"upstream.current_version", "package.upstream_version"} {
		if got, _ := getStringAtPath(raw, path); got != "3.46.0" {
			t.Errorf("%s = %q, want 3.46.0", path, got)
		}
	}
	// The pack's own release number is not the upstream one.
	if got, _ := getStringAtPath(raw, "package.version"); got != "0.1.0" {
		t.Errorf("package.version = %q, want 0.1.0 -- it must not be rewritten", got)
	}
}

func TestWritingIsANoOpWhenThePackShipsNoRecord(t *testing.T) {
	pack := &PackConfig{
		PackRoot: t.TempDir(), VersionFile: DefaultVersionFile, VersionKeys: DefaultVersionKeys(),
	}
	path, err := WriteVendoredVersion(pack, "3.46.0")
	if err != nil {
		t.Fatal(err)
	}
	if path != "" {
		t.Fatalf("got %q, want empty -- an absent file is a documented no-op", path)
	}
}

// TestWritingNeverCreatesAKey pins the rule that keeps the writer from
// inventing `package.upstream_version` in icon-sets-bundled, which ships none.
func TestWritingNeverCreatesAKey(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "resources", "assets", "svg")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "{\n  \"upstream\": {\n    \"current_version\": \"2.2.0\"\n  }\n}\n"
	target := filepath.Join(dir, "config.json")
	if err := os.WriteFile(target, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	pack := &PackConfig{
		PackRoot: root, VersionFile: DefaultVersionFile, VersionKeys: DefaultVersionKeys(),
	}

	if _, err := WriteVendoredVersion(pack, "2.3.0"); err != nil {
		t.Fatal(err)
	}

	raw, _ := os.ReadFile(target)
	if strings.Contains(string(raw), "upstream_version") {
		t.Fatalf("writer invented package.upstream_version:\n%s", raw)
	}
	if got, _ := getStringAtPath(raw, "upstream.current_version"); got != "2.3.0" {
		t.Fatalf("upstream.current_version = %q, want 2.3.0", got)
	}
}

// TestWritingPreservesEverythingElseByteForByte is the reason this package has
// no JSON dependency. Go map iteration has no order, so the obvious
// unmarshal-modify-marshal would reorder the whole document and bury a one-line
// version bump in a whole-file diff on the sync PR.
func TestWritingPreservesEverythingElseByteForByte(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "resources", "assets", "svg")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Deliberately not alphabetical, with a nested object and an array.
	before := `{
  "zulu": "last",
  "package": {
    "name": "ichava/icon-sets-demo",
    "keywords": ["icons", "svg"],
    "upstream_version": "1.0.0"
  },
  "alpha": {"nested": {"deep": true}},
  "upstream": {
    "current_version": "1.0.0",
    "cdn": {"jsdelivr": "https://cdn.example/{version}/{name}.svg"}
  }
}
`
	target := filepath.Join(dir, "config.json")
	if err := os.WriteFile(target, []byte(before), 0o644); err != nil {
		t.Fatal(err)
	}
	pack := &PackConfig{
		PackRoot: root, VersionFile: DefaultVersionFile, VersionKeys: DefaultVersionKeys(),
	}

	if _, err := WriteVendoredVersion(pack, "2.0.0"); err != nil {
		t.Fatal(err)
	}
	afterBytes, _ := os.ReadFile(target)
	after := string(afterBytes)

	want := strings.ReplaceAll(before, `"1.0.0"`, `"2.0.0"`)
	if after != want {
		t.Fatalf("document changed beyond the two versions.\n--- got ---\n%s\n--- want ---\n%s", after, want)
	}
}

func TestInterpolateLeavesUnknownTokensAlone(t *testing.T) {
	got := Interpolate("{pack_root}/files/{version}/{unknown}", map[string]string{
		"pack_root": "/work/demo", "version": "1.2.3",
	})
	want := "/work/demo/files/1.2.3/{unknown}"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestLoadPackDropsCommentKeys covers the `_`-prefixed authoring annotations
// every shipped config carries. The schema is otherwise closed, so without the
// strip none of them would load at all.
func TestLoadPackDropsCommentKeys(t *testing.T) {
	dir := t.TempDir()
	body := `{
  "_note": "this is an authoring comment, not config",
  "_status": "active",
  "name": "demo",
  "pack": "ichava/icon-sets-demo",
  "pack_root": "/work/demo",
  "version_check_url": "https://registry.example/demo",
  "source": {"type": "url", "version_field": "dist-tags.latest"}
}`
	if err := os.WriteFile(filepath.Join(dir, "demo.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	pack, err := LoadPack("demo", dir)
	if err != nil {
		t.Fatal(err)
	}
	if pack.Name != "demo" {
		t.Errorf("Name = %q", pack.Name)
	}
	// extra="allow" on the source block: version_field has to survive, because
	// the checker reads it for every url-typed pack.
	if got := pack.Source.VersionField(); got != "dist-tags.latest" {
		t.Errorf("VersionField = %q, want dist-tags.latest", got)
	}
	if pack.VersionFile != DefaultVersionFile {
		t.Errorf("VersionFile default not applied: %q", pack.VersionFile)
	}
	if len(pack.VersionKeys) != 2 {
		t.Errorf("VersionKeys default not applied: %v", pack.VersionKeys)
	}
}

func TestLoadPackRejectsAnUnknownTopLevelKey(t *testing.T) {
	dir := t.TempDir()
	body := `{
  "name": "demo", "pack": "ichava/demo", "pack_root": "/work/demo",
  "version_check_url": "https://x", "source": {"type": "npm"},
  "typo_here": true
}`
	if err := os.WriteFile(filepath.Join(dir, "demo.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadPack("demo", dir); err == nil {
		t.Fatal("expected an error: the pack schema is closed, so a typo must fail loudly")
	}
}

func TestLoadPackRejectsAnUnknownSourceType(t *testing.T) {
	dir := t.TempDir()
	body := `{
  "name": "demo", "pack": "ichava/demo", "pack_root": "/work/demo",
  "version_check_url": "https://x", "source": {"type": "totally-fake"}
}`
	if err := os.WriteFile(filepath.Join(dir, "demo.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadPack("demo", dir)
	if err == nil || !strings.Contains(err.Error(), "source") {
		t.Fatalf("want an error naming the source block, got %v", err)
	}
}

func TestLoadPackAndRegistryNameTheMissingFile(t *testing.T) {
	dir := t.TempDir()
	if _, err := LoadPack("nope", dir); err == nil || !strings.Contains(err.Error(), "missing pack config") {
		t.Errorf("LoadPack error = %v", err)
	}
	if _, err := LoadRegistry(dir); err == nil || !strings.Contains(err.Error(), "missing pack registry") {
		t.Errorf("LoadRegistry error = %v", err)
	}
}

// TestLoadsEveryShippedConfig is the one that matters: the config directory is
// a mounted volume, so these four files are an external interface this port
// must accept unchanged. Fixtures would not catch a drift between the schema
// and what is actually committed.
func TestLoadsEveryShippedConfig(t *testing.T) {
	dir := filepath.Join("..", "..", "..", "..", "config")
	if _, err := os.Stat(dir); err != nil {
		t.Skipf("no config/ beside the module: %v", err)
	}

	registry, err := LoadRegistry(dir)
	if err != nil {
		t.Fatalf("the shipped registry does not load: %v", err)
	}
	if len(registry.Packs) == 0 {
		t.Fatal("registry lists no packs")
	}

	packs, err := LoadAll(dir)
	if err != nil {
		t.Fatalf("a shipped pack config does not load: %v", err)
	}
	for _, pack := range packs {
		if pack.Name == "" || pack.Pack == "" || pack.PackRoot == "" {
			t.Errorf("%s: a required field is empty: %+v", pack.Name, pack)
		}
		if pack.VersionCheckURL == "" {
			t.Errorf("%s: version_check_url is empty", pack.Name)
		}
		// PACK_SLUG in every sync-upstream.yml is the repository name, and the
		// config is looked up by that slug, so the two must agree.
		if _, err := LoadPack(pack.Name, dir); err != nil {
			t.Errorf("%s: config filename does not match its own name field: %v", pack.Name, err)
		}
	}
	t.Logf("loaded %d shipped pack configs", len(packs))
}

// TestSourceExtraSurvivesARoundTrip guards the one field that reaches the
// checker through the extra map.
func TestSourceExtraSurvivesARoundTrip(t *testing.T) {
	var src SourceConfig
	if err := json.Unmarshal([]byte(`{"type":"url","version_field":"a.b.c","misc":1}`), &src); err != nil {
		t.Fatal(err)
	}
	if src.VersionField() != "a.b.c" {
		t.Errorf("VersionField = %q", src.VersionField())
	}
	if _, ok := src.Extra["misc"]; !ok {
		t.Error("unknown keys must be kept, not dropped")
	}
}

func TestSourceVersionFieldDefaultsToVersion(t *testing.T) {
	var src SourceConfig
	if err := json.Unmarshal([]byte(`{"type":"url"}`), &src); err != nil {
		t.Fatal(err)
	}
	if src.VersionField() != "version" {
		t.Errorf("VersionField = %q, want version", src.VersionField())
	}
}
