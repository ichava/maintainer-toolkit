// Package config loads the per-pack JSON configs and the registry that lists
// them, and reads and writes the version a pack repo says it vendors.
//
// The config directory is a mounted volume in production -- each pack's
// sync-upstream.yml bind-mounts the toolkit checkout's config/ at /app/config --
// so this schema is an external interface, not an internal one. It is ported
// from the Pydantic models in core/config.py and must accept every file those
// models accept, unchanged.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SourceConfig says where to fetch upstream artefacts from.
//
// Unknown keys are kept rather than rejected, and that is load-bearing: the
// checker reads `version_field` out of them for `type: "url"`, and both shipped
// url-typed packs (icon-sets-bundled, icon-sets-emoji) rely on it. Python
// expressed this as `extra="allow"`.
type SourceConfig struct {
	Type       string   `json:"type"`
	Package    string   `json:"package,omitempty"`
	Owner      string   `json:"owner,omitempty"`
	Repo       string   `json:"repo,omitempty"`
	ArchiveURL string   `json:"archive_url,omitempty"`
	SourcePath string   `json:"source_path,omitempty"`
	Path       string   `json:"path,omitempty"`
	Args       []string `json:"args,omitempty"`

	// Extra holds keys with no field above. See the type comment.
	Extra map[string]any `json:"-"`
}

// SourceTypes is the closed set Python declared as a Literal. A config naming
// anything else is rejected at load time, before any network call.
var SourceTypes = []string{"npm", "github-archive", "github-tag", "github-release", "url", "script"}

// VersionField returns the dot-path into the version-check response for a
// url-typed source, defaulting to "version" exactly as the checker does.
func (s SourceConfig) VersionField() string {
	if v, ok := s.Extra["version_field"].(string); ok && v != "" {
		return v
	}
	return "version"
}

type sourceAlias SourceConfig

func (s *SourceConfig) UnmarshalJSON(data []byte) error {
	var alias sourceAlias
	if err := json.Unmarshal(data, &alias); err != nil {
		return err
	}
	*s = SourceConfig(alias)

	var all map[string]any
	if err := json.Unmarshal(data, &all); err != nil {
		return err
	}
	known := map[string]bool{
		"type": true, "package": true, "owner": true, "repo": true,
		"archive_url": true, "source_path": true, "path": true, "args": true,
	}
	for k, v := range all {
		if !known[k] {
			if s.Extra == nil {
				s.Extra = map[string]any{}
			}
			s.Extra[k] = v
		}
	}

	for _, t := range SourceTypes {
		if s.Type == t {
			return nil
		}
	}
	return fmt.Errorf("source: unknown type %q (want one of %s)", s.Type, strings.Join(SourceTypes, ", "))
}

// SinkConfig says where refreshed assets land.
type SinkConfig struct {
	Type     string `json:"type"`
	Root     string `json:"root,omitempty"`
	RepoRoot string `json:"repo_root,omitempty"`
	Branch   string `json:"branch,omitempty"`

	Extra map[string]any `json:"-"`
}

// SinkTypes is the closed set Python declared as a Literal.
var SinkTypes = []string{"filesystem", "git-branch"}

type sinkAlias SinkConfig

func (s *SinkConfig) UnmarshalJSON(data []byte) error {
	var alias sinkAlias
	if err := json.Unmarshal(data, &alias); err != nil {
		return err
	}
	*s = SinkConfig(alias)

	var all map[string]any
	if err := json.Unmarshal(data, &all); err != nil {
		return err
	}
	known := map[string]bool{"type": true, "root": true, "repo_root": true, "branch": true}
	for k, v := range all {
		if !known[k] {
			if s.Extra == nil {
				s.Extra = map[string]any{}
			}
			s.Extra[k] = v
		}
	}

	for _, t := range SinkTypes {
		if s.Type == t {
			return nil
		}
	}
	return fmt.Errorf("sink: unknown type %q (want one of %s)", s.Type, strings.Join(SinkTypes, ", "))
}

// PackConfig is one config/<slug>.json.
//
// Unlike the source and sink blocks this rejects unknown keys, so a typo fails
// loudly instead of being silently ignored. Python expressed that as
// `extra="forbid"`, which is also why the comment-key strip below has to happen
// before validation rather than after.
type PackConfig struct {
	Name            string           `json:"name"`
	Pack            string           `json:"pack"`
	PackRoot        string           `json:"pack_root"`
	CurrentVersion  string           `json:"current_version,omitempty"`
	VersionFile     string           `json:"version_file,omitempty"`
	VersionKeys     []string         `json:"version_keys,omitempty"`
	VersionCheckURL string           `json:"version_check_url"`
	Source          SourceConfig     `json:"source"`
	Sinks           []SinkConfig     `json:"sinks,omitempty"`
	Transforms      []map[string]any `json:"transforms,omitempty"`
}

// DefaultVersionFile and DefaultVersionKeys mirror the Python defaults. No
// shipped config overrides either, so changing them changes every pack at once.
const DefaultVersionFile = "resources/assets/svg/config.json"

// DefaultVersionKeys is returned by value; callers must not retain the slice.
func DefaultVersionKeys() []string {
	return []string{"upstream.current_version", "package.upstream_version"}
}

// PacksRegistry is config/packs.json.
type PacksRegistry struct {
	Packs []string `json:"packs"`
}

// DefaultConfigDir resolves the directory the pack configs live in.
//
// ICHAVA_DEV_CONFIG_DIR wins, which is how the Docker image points at the
// bind-mounted /app/config. Otherwise it is `config/` beside the executable,
// falling back to ./config -- Python derived this from the source file's
// location, which has no equivalent in a compiled binary.
func DefaultConfigDir() string {
	if dir := os.Getenv("ICHAVA_DEV_CONFIG_DIR"); dir != "" {
		return dir
	}
	if exe, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(exe), "config")
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate
		}
	}
	return "config"
}

// stripCommentKeys drops top-level keys beginning with an underscore.
//
// JSON has no comments, so the shipped configs carry `_note`, `_version_note`
// and `_status` as authoring annotations. They have to go before the
// unknown-key check, or every shipped config fails to load. Top level only,
// matching Python -- an underscore key inside `source` is still an error.
func stripCommentKeys(data []byte) ([]byte, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	for k := range raw {
		if strings.HasPrefix(k, "_") {
			delete(raw, k)
		}
	}
	return json.Marshal(raw)
}

// LoadPack reads and validates one pack config.
func LoadPack(slug, configDir string) (*PackConfig, error) {
	if configDir == "" {
		configDir = DefaultConfigDir()
	}
	packFile := filepath.Join(configDir, slug+".json")

	data, err := os.ReadFile(packFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("missing pack config: %s", packFile)
		}
		return nil, err
	}

	cleaned, err := stripCommentKeys(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", packFile, err)
	}

	var pack PackConfig
	dec := json.NewDecoder(strings.NewReader(string(cleaned)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&pack); err != nil {
		return nil, fmt.Errorf("%s: %w", packFile, err)
	}

	if pack.VersionFile == "" {
		pack.VersionFile = DefaultVersionFile
	}
	if len(pack.VersionKeys) == 0 {
		pack.VersionKeys = DefaultVersionKeys()
	}
	pack.PackRoot = expandUser(pack.PackRoot)

	return &pack, nil
}

// LoadRegistry reads and validates config/packs.json.
func LoadRegistry(configDir string) (*PacksRegistry, error) {
	if configDir == "" {
		configDir = DefaultConfigDir()
	}
	registryFile := filepath.Join(configDir, "packs.json")

	data, err := os.ReadFile(registryFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("missing pack registry: %s", registryFile)
		}
		return nil, err
	}

	var registry PacksRegistry
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&registry); err != nil {
		return nil, fmt.Errorf("%s: %w", registryFile, err)
	}
	for i, slug := range registry.Packs {
		if slug == "" {
			return nil, fmt.Errorf("%s: packs[%d] is empty", registryFile, i)
		}
	}
	return &registry, nil
}

// LoadAll loads every pack in the registry, in registry order.
func LoadAll(configDir string) ([]*PackConfig, error) {
	registry, err := LoadRegistry(configDir)
	if err != nil {
		return nil, err
	}
	packs := make([]*PackConfig, 0, len(registry.Packs))
	for _, slug := range registry.Packs {
		pack, err := LoadPack(slug, configDir)
		if err != nil {
			return nil, err
		}
		packs = append(packs, pack)
	}
	return packs, nil
}

// Interpolate replaces {key} placeholders with the given bindings.
//
// This is a replacement loop, not format-string semantics: an unknown token is
// left in the output verbatim rather than raising. Several templates in the
// shipped configs rely on that, carrying tokens a later stage substitutes.
func Interpolate(template string, bindings map[string]string) string {
	out := template
	for k, v := range bindings {
		out = strings.ReplaceAll(out, "{"+k+"}", v)
	}
	return out
}

func expandUser(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(path, "~"), "/"))
		}
	}
	return path
}

// VendoredVersionFile is the pack repo's own version record.
func VendoredVersionFile(pack *PackConfig) string {
	return filepath.Join(pack.PackRoot, pack.VersionFile)
}

// ReadVendoredVersion returns the version the pack repo says it vendors.
//
// Returns "" when the file is missing, unreadable, unparseable, or declares
// none of the keys -- callers then fall back to the toolkit config's
// current_version. Swallowing the error is deliberate and matches Python: a
// pack that has not been synced yet is an ordinary state, not a failure.
func ReadVendoredVersion(pack *PackConfig) string {
	target := VendoredVersionFile(pack)

	info, err := os.Stat(target)
	if err != nil || info.IsDir() {
		return ""
	}
	data, err := os.ReadFile(target)
	if err != nil {
		return ""
	}

	for _, dotted := range pack.VersionKeys {
		if s, ok := getStringAtPath(data, dotted); ok {
			if v := strings.TrimSpace(s); v != "" {
				return v
			}
		}
	}
	return ""
}

// ResolvedCurrentVersion is the version upstream is compared against.
//
// The pack repo wins. The toolkit config's current_version is a fallback for a
// pack with no vendored record yet: it lives in *this* repository, so a bump
// written during a sync run is discarded with the runner. That asymmetry is the
// whole reason the sync converges (V49).
func ResolvedCurrentVersion(pack *PackConfig) string {
	if v := ReadVendoredVersion(pack); v != "" {
		return v
	}
	return pack.CurrentVersion
}

// WriteVendoredVersion records version at every declared key in the pack repo.
//
// Returns the file written, or "" when the pack ships no such file. It only
// rewrites a leaf that already exists and already differs -- it never creates a
// key. That is what keeps it from inventing `package.upstream_version` in a
// pack that deliberately has none (icon-sets-bundled), and from touching
// `package.version`, which is the pack's own release number rather than the
// upstream one.
//
// The edit is surgical rather than parse-modify-reserialise. Go maps have no
// key order, so round-tripping the document would reorder every key in it and
// bury a one-line version bump in a whole-file diff on the sync PR.
func WriteVendoredVersion(pack *PackConfig, version string) (string, error) {
	target := VendoredVersionFile(pack)

	info, err := os.Stat(target)
	if err != nil || info.IsDir() {
		return "", nil //nolint:nilerr // absent file is a documented no-op, not a failure
	}

	data, err := os.ReadFile(target)
	if err != nil {
		return "", err
	}
	if !json.Valid(data) {
		return "", fmt.Errorf("%s: not valid JSON", target)
	}

	changed := false
	for _, dotted := range pack.VersionKeys {
		existing, ok := getStringAtPath(data, dotted)
		if !ok || existing == version {
			continue
		}
		updated, ok, err := setStringAtPath(data, dotted, version)
		if err != nil {
			return "", fmt.Errorf("%s: setting %s: %w", target, dotted, err)
		}
		if ok {
			data = updated
			changed = true
		}
	}

	if changed {
		if err := os.WriteFile(target, data, 0o644); err != nil {
			return "", err
		}
	}
	return target, nil
}
