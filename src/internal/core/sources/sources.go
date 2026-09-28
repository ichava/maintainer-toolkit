// Package sources fetches upstream artefacts into the pipeline's working
// directory and records where they landed.
package sources

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ichava/maintainer-toolkit/src/internal/core/httpx"
	"github.com/ichava/maintainer-toolkit/src/internal/core/pipeline"
)

// Downloader is the HTTP surface the sources need.
type Downloader interface {
	Download(ctx context.Context, url, dest string, force bool) (string, error)
}

// NpmTarball fetches a package with `npm pack` and extracts it.
//
// Shells out to npm rather than resolving the registry directly: npm handles
// scoped names, auth and the tarball URL indirection, and the image already
// ships it.
type NpmTarball struct {
	pipeline.SourceKind

	Package string
	Version string
	Log     *slog.Logger
}

func (NpmTarball) Name() string { return "NpmTarball" }

func (s NpmTarball) Execute(ctx *pipeline.Context) error {
	if s.Package == "" {
		return fmt.Errorf("NpmTarball: package is required")
	}
	if s.Version == "" {
		return fmt.Errorf("NpmTarball: version is required")
	}

	log := s.Log
	if log == nil {
		log = slog.Default()
	}

	workDir := filepath.Join(ctx.WorkingDir, "npm")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return err
	}

	spec := s.Package + "@" + s.Version
	log.Info("npm pack", "spec", spec)

	cmd := exec.Command("npm", "pack", spec, "--silent")
	cmd.Dir = workDir
	out, err := cmd.Output()
	if err != nil {
		if _, lookErr := exec.LookPath("npm"); lookErr != nil {
			return fmt.Errorf("npm not on PATH; the maintainer-toolkit image installs it -- mount issue?")
		}
		if ee, ok := err.(*exec.ExitError); ok {
			return fmt.Errorf("npm pack failed: %s", strings.TrimSpace(string(ee.Stderr)))
		}
		return fmt.Errorf("npm pack failed: %w", err)
	}

	// npm prints the tarball name on stdout; --silent suppresses everything
	// else, but take the last non-empty line rather than the whole output in
	// case a warning still slips through.
	tarball := ""
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			tarball = line
		}
	}
	if tarball == "" {
		return fmt.Errorf("npm pack printed no tarball name for %s", spec)
	}

	target := filepath.Join(workDir, "extracted")
	if err := ExtractTarGz(filepath.Join(workDir, tarball), target); err != nil {
		return err
	}

	// Every npm tarball wraps its contents in package/.
	pkgDir := filepath.Join(target, "package")
	if info, err := os.Stat(pkgDir); err != nil || !info.IsDir() {
		return fmt.Errorf("npm tarball %s has no package/ directory", tarball)
	}

	ctx.SetFetchedPath(pkgDir)
	// Record what npm resolved, not what was asked for. A set tracking
	// "latest" would otherwise stamp the literal string "latest" as its
	// version, which is not a version and compares as one.
	ctx.SetFetchedVersion(resolvedVersion(tarball, s.Package, s.Version))
	ctx.SetString(pipeline.KeyFetchedSource, "npm:"+spec)
	return nil
}

// resolvedVersion reads the version out of the filename npm produced.
//
// `npm pack` names the file <name>-<version>.tgz with the scope stripped and
// its slash replaced by a dash. Falls back to the requested version, which is
// right whenever that was already concrete.
func resolvedVersion(tarball, pkg, requested string) string {
	base := strings.TrimSuffix(tarball, ".tgz")

	name := pkg
	if i := strings.Index(name, "/"); i >= 0 {
		name = strings.TrimPrefix(name[:i], "@") + "-" + name[i+1:]
	}
	if v := strings.TrimPrefix(base, name+"-"); v != base && v != "" {
		return v
	}
	return requested
}

// GithubArchive downloads a tagged archive and extracts it.
type GithubArchive struct {
	pipeline.SourceKind

	// ArchiveURL is a template with {version}. When empty it is built from
	// Owner, Repo and Version -- note the built form hardcodes a `v` prefix,
	// which is why a project tagging without one has to supply the template.
	ArchiveURL string
	Owner      string
	Repo       string
	Version    string

	HTTP Downloader
	Log  *slog.Logger
}

func (GithubArchive) Name() string { return "GithubArchive" }

func (s GithubArchive) Execute(ctx *pipeline.Context) error {
	if s.ArchiveURL == "" && (s.Owner == "" || s.Repo == "" || s.Version == "") {
		return fmt.Errorf("GithubArchive: needs archive_url, or all of owner, repo and version")
	}
	if s.Version == "" {
		return fmt.Errorf("GithubArchive: version is required")
	}

	log := s.Log
	if log == nil {
		log = slog.Default()
	}
	client := s.HTTP
	if client == nil {
		client = httpx.New()
	}

	template := s.ArchiveURL
	if template == "" {
		template = fmt.Sprintf("https://github.com/%s/%s/archive/refs/tags/v{version}.zip", s.Owner, s.Repo)
	}
	url := strings.ReplaceAll(template, "{version}", s.Version)

	workDir := filepath.Join(ctx.WorkingDir, "gh-archive")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return err
	}
	archive := filepath.Join(workDir, "src.archive")

	log.Info("downloading", "url", url)
	if _, err := client.Download(context.Background(), url, archive, false); err != nil {
		return err
	}

	target := filepath.Join(workDir, "extracted")
	if err := ExtractZip(archive, target); err != nil {
		// GitHub serves .zip for the tags URL, but a project can point
		// archive_url at a tarball instead.
		if tarErr := ExtractTarGz(archive, target); tarErr != nil {
			return fmt.Errorf("%s is neither a zip (%v) nor a gzipped tar (%v)", url, err, tarErr)
		}
	}
	_ = os.Remove(archive)

	root, err := singleExtractedRoot(target)
	if err != nil {
		return err
	}

	ctx.SetFetchedPath(root)
	ctx.SetFetchedVersion(s.Version)
	ctx.SetString(pipeline.KeyFetchedSource,
		fmt.Sprintf("github-archive:%s/%s@v%s", s.Owner, s.Repo, s.Version))
	return nil
}

// singleExtractedRoot returns the one directory an archive unpacked into.
//
// A GitHub archive wraps everything in <repo>-<tag>/, and the name embeds the
// tag, so it cannot be predicted from the config alone. More than one root
// means the archive is not the shape this expects and guessing would pick the
// wrong tree.
func singleExtractedRoot(target string) (string, error) {
	entries, err := os.ReadDir(target)
	if err != nil {
		return "", err
	}

	var dirs []string
	for _, e := range entries {
		// __MACOSX is resource-fork noise a zip made on macOS carries.
		if e.IsDir() && e.Name() != "__MACOSX" {
			dirs = append(dirs, e.Name())
		}
	}
	if len(dirs) != 1 {
		return "", fmt.Errorf("expected exactly one extracted root under %s, got %v", target, dirs)
	}
	return filepath.Join(target, dirs[0]), nil
}

// UnicodeCldr downloads the emoji taxonomy the categoriser needs.
type UnicodeCldr struct {
	pipeline.SourceKind

	// UnicodeVersion is the emoji release, e.g. "16.0".
	//
	// The default has bitten this project: it ran ahead of what Unicode had
	// published, every fetch 404'd, and the pack shipped a tagged release with
	// zero SVGs. Set it to a version that exists.
	UnicodeVersion string

	HTTP Downloader
	Log  *slog.Logger
}

func (UnicodeCldr) Name() string { return "UnicodeCldr" }

func (s UnicodeCldr) Execute(ctx *pipeline.Context) error {
	version := s.UnicodeVersion
	if version == "" {
		return fmt.Errorf("UnicodeCldr: unicode_version is required")
	}

	client := s.HTTP
	if client == nil {
		client = httpx.New()
	}
	log := s.Log
	if log == nil {
		log = slog.Default()
	}

	url := fmt.Sprintf("https://unicode.org/Public/emoji/%s/emoji-test.txt", version)
	dest := filepath.Join(ctx.WorkingDir, fmt.Sprintf("emoji-test-%s.txt", version))

	log.Info("downloading emoji taxonomy", "url", url)
	if _, err := client.Download(context.Background(), url, dest, false); err != nil {
		return err
	}

	text, err := os.ReadFile(dest)
	if err != nil {
		return err
	}

	// Deliberately does not set fetched_path: this is a taxonomy, not assets.
	// The asset source runs alongside it and owns that key.
	ctx.SetString(pipeline.KeyCLDRPath, dest)
	ctx.SetString(pipeline.KeyCLDRText, string(text))
	ctx.SetString(pipeline.KeyCLDRVersion, version)
	return nil
}

// Compound runs two sources in order.
//
// A pipeline takes exactly one source, and the emoji recipe needs both an
// asset fetch and the CLDR taxonomy. The taxonomy runs first so the asset
// source owns fetched_path -- reverse them and the categoriser is handed the
// taxonomy file as its tree.
type Compound struct {
	pipeline.SourceKind
	First, Second pipeline.Source
}

func (c Compound) Name() string {
	return fmt.Sprintf("Compound(%s,%s)", c.First.Name(), c.Second.Name())
}

func (c Compound) Execute(ctx *pipeline.Context) error {
	if err := c.First.Execute(ctx); err != nil {
		return err
	}
	return c.Second.Execute(ctx)
}

// Static satisfies the pipeline's "must have a source" rule for a commit-only
// pipeline, which has nothing to fetch but still has a version to record.
//
// It replaces Python's _NoOpSource, and the difference is a bug fix rather
// than a rename. Each pipeline gets its own context, so nothing the emoji
// recipe's three asset pipelines set reaches its trailing commit pipeline.
// With a source that set nothing, VersionStamp found no fetched_version,
// logged "skipping" and did nothing -- so icon-sets-emoji was never stamped at
// all, and its sync could not converge. That is precisely the V49 failure the
// vendored-version design exists to prevent, still live in the one recipe that
// composes several pipelines.
type Static struct {
	pipeline.SourceKind
	Version string
}

func (Static) Name() string { return "StaticSource" }

func (s Static) Execute(ctx *pipeline.Context) error {
	if s.Version != "" {
		ctx.SetFetchedVersion(s.Version)
	}
	return nil
}

// --- archive extraction ---

// safeJoin resolves an archive member against its destination and refuses any
// path that escapes it.
//
// A tar or zip entry named ../../etc/passwd is the Zip Slip traversal. Python
// got this from tarfile's filter="data"; Go's archive packages do nothing, so
// it has to be explicit.
func safeJoin(dest, name string) (string, error) {
	cleaned := filepath.Join(dest, filepath.Clean("/"+name))
	if !strings.HasPrefix(cleaned, filepath.Clean(dest)+string(os.PathSeparator)) &&
		cleaned != filepath.Clean(dest) {
		return "", fmt.Errorf("archive entry %q escapes the destination", name)
	}
	return cleaned, nil
}

// ExtractTarGz unpacks a gzipped tar, refusing any entry that escapes dest.
func ExtractTarGz(archive, dest string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}

		path, err := safeJoin(dest, hdr.Name)
		if err != nil {
			return err
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(path, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := writeMember(path, tr, os.FileMode(hdr.Mode)); err != nil {
				return err
			}
			// Symlinks and every other type are skipped: an icon pack has no
			// legitimate use for one, and following it is how an extraction
			// writes outside its destination.
		}
	}
}

// ExtractZip unpacks a zip, refusing any entry that escapes dest.
func ExtractZip(archive, dest string) error {
	r, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, f := range r.File {
		path, err := safeJoin(dest, f.Name)
		if err != nil {
			return err
		}

		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(path, 0o755); err != nil {
				return err
			}
			continue
		}
		if !f.Mode().IsRegular() {
			continue
		}

		rc, err := f.Open()
		if err != nil {
			return err
		}
		err = writeMember(path, rc, f.Mode())
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func writeMember(path string, src io.Reader, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if mode == 0 {
		mode = 0o644
	}
	out, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode.Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, src); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
