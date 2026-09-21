package sources

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSafeJoinRefusesTraversal is the Zip Slip guard.
//
// Python got this free from tarfile's filter="data"; Go's archive packages do
// nothing about it, so an upstream tarball with an entry named
// ../../../etc/something would otherwise be written wherever it pointed. The
// toolkit runs against archives fetched from the internet, inside a container
// that has a mounted pack tree, so it matters.
func TestSafeJoinRefusesTraversal(t *testing.T) {
	dest := "/tmp/extract"

	for _, name := range []string{
		"../escape.svg",
		"../../etc/passwd",
		"a/../../escape.svg",
		"/absolute/escape.svg",
	} {
		got, err := safeJoin(dest, name)
		// An absolute or traversing name must either be refused, or be clamped
		// back inside the destination -- never resolve outside it.
		if err == nil && !strings.HasPrefix(got, dest) {
			t.Errorf("safeJoin(%q) = %q, which escapes %q", name, got, dest)
		}
	}

	ok, err := safeJoin(dest, "icons/home.svg")
	if err != nil {
		t.Fatalf("an ordinary entry was refused: %v", err)
	}
	if ok != filepath.Join(dest, "icons/home.svg") {
		t.Errorf("safeJoin = %q", ok)
	}
}

func writeTarGz(t *testing.T, path string, files map[string]string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{
			Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg,
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestExtractTarGz(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "a.tgz")
	writeTarGz(t, archive, map[string]string{
		"package/icons/home.svg": "<svg/>",
		"package/package.json":   "{}",
	})

	dest := filepath.Join(dir, "out")
	if err := extractTarGz(archive, dest); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(filepath.Join(dest, "package", "icons", "home.svg"))
	if err != nil || string(got) != "<svg/>" {
		t.Fatalf("extracted %q, err %v", got, err)
	}
}

// TestExtractTarGzRefusesATraversingEntry is the end-to-end of the guard.
func TestExtractTarGzRefusesATraversingEntry(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "evil.tgz")
	writeTarGz(t, archive, map[string]string{"../escaped.svg": "<svg/>"})

	dest := filepath.Join(dir, "out")
	_ = extractTarGz(archive, dest)

	if _, err := os.Stat(filepath.Join(dir, "escaped.svg")); err == nil {
		t.Fatal("a traversing archive entry was written outside the destination")
	}
}

func TestExtractZip(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "a.zip")

	f, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, err := zw.Create("openmoji-15.1.0/color/svg/1F600.svg")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("<svg/>")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()

	dest := filepath.Join(dir, "out")
	if err := extractZip(archive, dest); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dest, "openmoji-15.1.0", "color", "svg", "1F600.svg")); err != nil {
		t.Fatal(err)
	}
}

// TestSingleExtractedRoot covers the wrapper directory a GitHub archive adds,
// whose name embeds the tag and so cannot be predicted from the config.
func TestSingleExtractedRoot(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "openmoji-15.1.0"), 0o755); err != nil {
		t.Fatal(err)
	}
	// A zip made on macOS carries this; it must not count as a second root.
	if err := os.MkdirAll(filepath.Join(dir, "__MACOSX"), 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := singleExtractedRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(got) != "openmoji-15.1.0" {
		t.Errorf("root = %s", got)
	}

	if err := os.MkdirAll(filepath.Join(dir, "second"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := singleExtractedRoot(dir); err == nil {
		t.Error("two roots must be an error rather than a guess at which is wanted")
	}
}

func TestUnicodeCldrRequiresAVersion(t *testing.T) {
	// The default once ran ahead of what Unicode had published, every fetch
	// 404'd, and the pack shipped a tagged release with zero SVGs. There is
	// no default now.
	if err := (UnicodeCldr{}).Execute(nil); err == nil {
		t.Fatal("expected an error naming the missing unicode_version")
	}
}
