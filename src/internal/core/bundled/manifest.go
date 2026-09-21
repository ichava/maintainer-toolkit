package bundled

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Manifest records where each vendored set comes from.
//
// It lives in its own file beside the pack config -- config/<pack>.sets.json --
// rather than as a key inside it. That is deliberate and temporary: the Python
// is still the Docker entrypoint, its PackConfig rejects unknown keys, and a
// new key in the shipped config would break every Python command for this pack
// during the overlap. A separate file the Python never opens has no such
// effect. Fold it in once the Python is gone.
type Manifest struct {
	// Pack is the composer name, recorded so a manifest cannot be pointed at
	// the wrong pack by being copied.
	Pack string `json:"pack"`

	// Sets maps a vendored set directory to its upstream.
	Sets map[string]SetSource `json:"sets"`
}

// SetSource is one set's upstream.
type SetSource struct {
	// Package is the npm package the icons come from.
	Package string `json:"package"`

	// Version pins the release. Empty means the registry's `latest`, which is
	// what a refresh normally wants; a pin is for a set whose upstream has
	// changed format and whose migration has not been taken yet.
	Version string `json:"version,omitempty"`

	// Path is the directory inside the extracted package holding the SVGs.
	Path string `json:"path"`

	// Confidence is how the entry was established, carried over from the audit
	// so a reader can tell a measured entry from an asserted one. An entry
	// written by hand and never verified should say so.
	Confidence Confidence `json:"confidence,omitempty"`

	// Note carries anything a human needs to know before trusting it.
	Note string `json:"note,omitempty"`
}

// ManifestPath is the conventional location beside a pack config.
func ManifestPath(configDir, packName string) string {
	return filepath.Join(configDir, packName+".sets.json")
}

// LoadManifest reads the manifest for a pack.
func LoadManifest(configDir, packName string) (*Manifest, error) {
	path := ManifestPath(configDir, packName)

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("missing set manifest: %s -- run `imt bundled audit --json` to build one", path)
		}
		return nil, err
	}

	var m Manifest
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if len(m.Sets) == 0 {
		return nil, fmt.Errorf("%s declares no sets", path)
	}

	for name, src := range m.Sets {
		if src.Package == "" {
			return nil, fmt.Errorf("%s: set %q has no package", path, name)
		}
	}
	return &m, nil
}

// Save writes a manifest, sorted and indented so a diff shows only what moved.
func (m *Manifest) Save(path string) error {
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(m); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

// SetNames returns the sets in a stable order.
func (m *Manifest) SetNames() []string {
	names := make([]string, 0, len(m.Sets))
	for name := range m.Sets {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ManifestFromResolutions builds a manifest from an audit run.
//
// Only entries the audit actually established are included. A `partial` or
// `none` result is left out rather than written with a warning: a manifest is
// the input to something that overwrites a set directory, and an entry that is
// present but wrong replaces a set's icons with another project's. Absent means
// "not refreshed yet", which is safe; present-but-wrong is not.
func ManifestFromResolutions(pack string, rs []Resolution) *Manifest {
	m := &Manifest{Pack: pack, Sets: map[string]SetSource{}}

	for _, r := range rs {
		switch r.Confidence {
		case ConfidenceExact, ConfidenceStrong, ConfidencePackage:
		default:
			continue
		}
		m.Sets[r.Set] = SetSource{
			Package:    r.Package,
			Path:       r.Path,
			Confidence: r.Confidence,
			Note:       r.Note,
		}
	}
	return m
}

// Unresolved returns the sets present in the pack but absent from the manifest.
//
// A refresh reports these rather than skipping them silently: an aggregator
// pack that quietly stops refreshing a third of itself is the failure this
// whole exercise exists to avoid.
func (m *Manifest) Unresolved(sets []string) []string {
	var missing []string
	for _, s := range sets {
		if _, ok := m.Sets[s]; !ok {
			missing = append(missing, s)
		}
	}
	sort.Strings(missing)
	return missing
}
