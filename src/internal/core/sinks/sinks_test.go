package sinks

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ichava/maintainer-toolkit/src/internal/core/pipeline"
)

func svgTree(t *testing.T, dir string, n int) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < n; i++ {
		name := filepath.Join(dir, "icon"+string(rune('a'+i%26))+string(rune('a'+i/26))+".svg")
		if err := os.WriteFile(name, []byte("<svg/>"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func ctxFor(t *testing.T, source string) *pipeline.Context {
	t.Helper()
	c := &pipeline.Context{
		PipelineName: "test", WorkingDir: t.TempDir(),
		Config: map[string]any{}, Extras: map[string]any{}, Metrics: map[string]any{},
	}
	c.SetFetchedPath(source)
	return c
}

// TestRetentionGuardRefusesALargeShrink is the iconoir case: upstream
// reorganised into per-weight directories, so a manifest path that used to
// cover the whole set now covers a fraction of it. The sink wipes before it
// copies, so without this the refresh deletes the difference and reports
// success.
func TestRetentionGuardRefusesALargeShrink(t *testing.T) {
	target := svgTree(t, filepath.Join(t.TempDir(), "dest"), 100)
	source := svgTree(t, filepath.Join(t.TempDir(), "src"), 60)

	err := Filesystem{Root: target, MinRetention: 0.90}.Execute(ctxFor(t, source))
	if err == nil {
		t.Fatal("expected a refusal")
	}

	// And the destination is untouched, which is the whole point of counting
	// before removing.
	n, _ := countSVGs(target)
	if n != 100 {
		t.Errorf("destination now has %d icons, want 100 left exactly as they were", n)
	}
}

func TestRetentionGuardAllowsAnOrdinaryShrink(t *testing.T) {
	target := svgTree(t, filepath.Join(t.TempDir(), "dest"), 100)
	source := svgTree(t, filepath.Join(t.TempDir(), "src"), 95)

	if err := (Filesystem{Root: target, MinRetention: 0.90}).Execute(ctxFor(t, source)); err != nil {
		t.Fatalf("an upstream retiring five icons must not fail a refresh: %v", err)
	}
	if n, _ := countSVGs(target); n != 95 {
		t.Errorf("destination has %d icons, want 95", n)
	}
}

func TestRetentionGuardIsOffByDefault(t *testing.T) {
	target := svgTree(t, filepath.Join(t.TempDir(), "dest"), 100)
	source := svgTree(t, filepath.Join(t.TempDir(), "src"), 1)

	// The single-upstream packs replace a whole tree from one source and have
	// no use for the guard; it must not change their behaviour.
	if err := (Filesystem{Root: target}).Execute(ctxFor(t, source)); err != nil {
		t.Fatalf("the guard must be opt-in: %v", err)
	}
}

func TestRetentionGuardIgnoresAnEmptyDestination(t *testing.T) {
	target := filepath.Join(t.TempDir(), "dest")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	source := svgTree(t, filepath.Join(t.TempDir(), "src"), 5)

	if err := (Filesystem{Root: target, MinRetention: 0.90}).Execute(ctxFor(t, source)); err != nil {
		t.Fatalf("a first sync has nothing to protect: %v", err)
	}
}
