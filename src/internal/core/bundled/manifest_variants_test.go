package bundled

import (
	"os"
	"strings"
	"testing"
)

// saveAndLoad round-trips a manifest through the real loader, which is where
// the validation lives.
func saveAndLoad(t *testing.T, sets map[string]SetSource) (*Manifest, error) {
	t.Helper()
	dir := t.TempDir()
	m := &Manifest{Pack: "ichava/icon-sets-bundled", Sets: sets}
	if err := m.Save(ManifestPath(dir, "icon-sets-bundled")); err != nil {
		t.Fatal(err)
	}
	return LoadManifest(dir, "icon-sets-bundled")
}

// TestManifestAcceptsAFlattenedSet is the heroicons entry as it is written.
func TestManifestAcceptsAFlattenedSet(t *testing.T) {
	m, err := saveAndLoad(t, map[string]SetSource{
		"heroicons": {
			Package: "heroicons",
			Variants: []Variant{
				{Path: "24/outline", Prefix: "o-"},
				{Path: "24/solid", Prefix: "s-"},
			},
			Confidence: ConfidenceExact,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := m.Sets["heroicons"].Variants; len(got) != 2 || got[1].Prefix != "s-" {
		t.Errorf("variants did not survive the round trip: %+v", got)
	}
}

// TestManifestRefusesBothForms guards the ambiguity, and it is not cosmetic:
// the refresh would have to pick one silently, and the two produce different
// sets -- a single path refreshes one variant while the sink deletes the rest.
func TestManifestRefusesBothForms(t *testing.T) {
	_, err := saveAndLoad(t, map[string]SetSource{
		"heroicons": {
			Package:  "heroicons",
			Path:     "24/solid",
			Variants: []Variant{{Path: "24/outline", Prefix: "o-"}},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "not both") {
		t.Fatalf("a set declaring both forms must be refused, got %v", err)
	}
}

// TestManifestRefusesNeitherForm stops an entry resolving to the package root,
// which for most upstreams is a README and a licence rather than icons.
func TestManifestRefusesNeitherForm(t *testing.T) {
	_, err := saveAndLoad(t, map[string]SetSource{
		"heroicons": {Package: "heroicons"},
	})
	if err == nil || !strings.Contains(err.Error(), "not neither") {
		t.Fatalf("a set declaring no source directory must be refused, got %v", err)
	}
}

// TestManifestRefusesAVariantWithNoPath covers the half-written entry.
func TestManifestRefusesAVariantWithNoPath(t *testing.T) {
	_, err := saveAndLoad(t, map[string]SetSource{
		"heroicons": {
			Package:  "heroicons",
			Variants: []Variant{{Prefix: "o-"}},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "no path") {
		t.Fatalf("a variant without a path must be refused, got %v", err)
	}
}

// TestManifestRejectsAnUnknownKey keeps a typo from being read as an absent
// option. `variant` for `variants` would otherwise load as a set with neither
// form -- caught here, but by the wrong error.
func TestManifestRejectsAnUnknownKey(t *testing.T) {
	dir := t.TempDir()
	body := `{"pack":"ichava/icon-sets-bundled","sets":{"x":{"package":"x","variant":[]}}}`
	if err := os.WriteFile(ManifestPath(dir, "icon-sets-bundled"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadManifest(dir, "icon-sets-bundled"); err == nil {
		t.Fatal("an unknown key must be refused")
	}
}
