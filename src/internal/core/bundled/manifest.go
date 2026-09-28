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

// Variant is one upstream directory inside a flattened set, with the affix
// that distinguishes its icons once every variant shares a directory.
type Variant struct {
	// Path is the directory inside the extracted package.
	Path string `json:"path"`

	// Prefix and Suffix mark this variant's filenames. A variant carrying
	// neither is the unmarked one, which is how teeny-icons spells `solid`.
	Prefix string `json:"prefix,omitempty"`
	Suffix string `json:"suffix,omitempty"`
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
	// Mutually exclusive with Variants.
	Path string `json:"path,omitempty"`

	// Variants merges several upstream directories into one flat set, which
	// is what a pack does when it vendors a multi-variant upstream under one
	// directory and distinguishes the variants by an affix on the filename.
	//
	// A single Path cannot express that, and the failure is not a partial
	// refresh but a destructive one: the sink wipes the set first, so
	// pointing Path at heroicons' 24/solid would refresh a quarter of the
	// icons and delete the other three quarters. Three sets in this pack are
	// this shape -- heroicons, google-material-design-icons and teeny-icons.
	Variants []Variant `json:"variants,omitempty"`

	// Separator normalises the character upstream puts between words when the
	// pack spells them differently. Only google-material-design-icons needs
	// it: upstream ships `18_up_rating` against the pack's `18-up-rating`.
	Separator string `json:"separator,omitempty"`

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
		// Exactly one form. Both would leave it ambiguous which the refresh
		// honours, and neither leaves it pointed at the package root, which
		// for most upstreams is a README and a licence rather than icons.
		if (src.Path == "") == (len(src.Variants) == 0) {
			return nil, fmt.Errorf(
				"%s: set %q must declare either `path` or `variants`, not both and not neither",
				path, name)
		}
		for i, v := range src.Variants {
			if v.Path == "" {
				return nil, fmt.Errorf("%s: set %q variant %d has no path", path, name, i)
			}
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
