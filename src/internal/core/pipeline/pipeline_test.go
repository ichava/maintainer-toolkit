package pipeline

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The five cases below are ports of tests/unit/test_pipeline.py. The ones after
// them cover behaviour Python had but never asserted.

type recordingSource struct {
	SourceKind
	fired *bool
}

func (recordingSource) Name() string { return "RecordingSource" }
func (s recordingSource) Execute(ctx *Context) error {
	dir := filepath.Join(ctx.WorkingDir, "rec")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "a.svg"), []byte("<svg/>"), 0o644); err != nil {
		return err
	}
	ctx.SetFetchedPath(dir)
	if s.fired != nil {
		*s.fired = true
	}
	return nil
}

type flaggingTransform struct {
	TransformKind
	fired *bool
}

func (flaggingTransform) Name() string { return "FlaggingTransform" }
func (t flaggingTransform) Execute(ctx *Context) error {
	*t.fired = true
	ctx.Metric("flagging", map[string]any{"ran": true})
	return nil
}

type failingTransform struct {
	TransformKind
}

func (failingTransform) Name() string             { return "FailingTransform" }
func (failingTransform) Execute(_ *Context) error { return errors.New("boom") }

type copyingSink struct {
	SinkKind
	fired  *bool
	target string
}

func (copyingSink) Name() string { return "CopyingSink" }
func (s copyingSink) Execute(ctx *Context) error {
	*s.fired = true
	src, ok := ctx.FetchedPath()
	if !ok {
		return errors.New("no fetched_path")
	}
	data, err := os.ReadFile(filepath.Join(src, "a.svg"))
	if err != nil {
		return err
	}
	return os.WriteFile(s.target, data, 0o644)
}

func TestPipelineRunsEachStageInOrder(t *testing.T) {
	target := filepath.Join(t.TempDir(), "out.svg")
	var transformFired, sinkFired bool

	result, err := Named("demo").
		Source(recordingSource{}).
		Transform(flaggingTransform{fired: &transformFired}).
		Sink(copyingSink{fired: &sinkFired, target: target}).
		Run()
	if err != nil {
		t.Fatal(err)
	}

	if !result.Success {
		t.Fatalf("run failed: %s", result.Error())
	}
	if !transformFired || !sinkFired {
		t.Fatalf("transform fired=%v sink fired=%v, want both", transformFired, sinkFired)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "<svg/>" {
		t.Fatalf("sink wrote %q", data)
	}
}

func TestPipelineRequiresASource(t *testing.T) {
	var fired bool
	_, err := Named("demo").Sink(copyingSink{fired: &fired}).Run()
	if err == nil || !strings.Contains(err.Error(), "no source") {
		t.Fatalf("err = %v, want one naming the missing source", err)
	}
}

func TestPipelineRequiresASink(t *testing.T) {
	_, err := Named("demo").Source(recordingSource{}).Run()
	if err == nil || !strings.Contains(err.Error(), "no sinks") {
		t.Fatalf("err = %v, want one naming the missing sinks", err)
	}
}

// TestPipelineCapturesStageFailure is the important one: a failure must stop
// the run, so a sink never commits assets a transform failed to produce.
func TestPipelineCapturesStageFailure(t *testing.T) {
	var sinkFired bool

	result, err := Named("demo").
		Source(recordingSource{}).
		Transform(failingTransform{}).
		Sink(copyingSink{fired: &sinkFired, target: filepath.Join(t.TempDir(), "out.svg")}).
		Run()
	if err != nil {
		t.Fatalf("a stage failure is a Result, not a Run error: %v", err)
	}

	if result.Success {
		t.Fatal("result reports success despite a failing stage")
	}
	if !strings.Contains(result.Error(), "boom") {
		t.Errorf("error = %q, want it to carry the cause", result.Error())
	}
	if result.FailedStage != "FailingTransform" {
		t.Errorf("FailedStage = %q", result.FailedStage)
	}
	if sinkFired {
		t.Error("the sink ran after an upstream stage failed")
	}
}

func TestPipelineDoubleSourceIsAnError(t *testing.T) {
	var fired bool
	_, err := Named("demo").
		Source(recordingSource{}).
		Source(recordingSource{}).
		Sink(copyingSink{fired: &fired}).
		Run()
	if err == nil || !strings.Contains(err.Error(), "already has a source") {
		t.Fatalf("err = %v, want one naming the duplicate source", err)
	}
}

// TestPipelineRemovesOnlyTheDirectoryItCreated covers the ownership rule the
// Python `created_tempdir` flag encoded. A caller-supplied working directory
// may be one they are inspecting; deleting it would be a surprise.
func TestPipelineRemovesOnlyTheDirectoryItCreated(t *testing.T) {
	supplied := t.TempDir()
	var fired bool

	if _, err := Named("demo").
		Source(recordingSource{}).
		Sink(copyingSink{fired: &fired, target: filepath.Join(supplied, "out.svg")}).
		WorkingDir(supplied).
		Run(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(supplied); err != nil {
		t.Fatalf("a caller-supplied working dir was removed: %v", err)
	}

	// And the opposite: one the pipeline created is gone afterwards.
	var captured string
	capture := capturingSink{path: &captured}
	if _, err := Named("demo").Source(recordingSource{}).Sink(capture).Run(); err != nil {
		t.Fatal(err)
	}
	if captured == "" {
		t.Fatal("sink did not observe a working dir")
	}
	if _, err := os.Stat(captured); !os.IsNotExist(err) {
		t.Fatalf("pipeline-created working dir %s still exists", captured)
	}
}

type capturingSink struct {
	SinkKind
	path *string
}

func (capturingSink) Name() string { return "CapturingSink" }
func (s capturingSink) Execute(ctx *Context) error {
	*s.path = ctx.WorkingDir
	return nil
}

func TestResultSummaryDropsBytePayloads(t *testing.T) {
	r := Result{
		PipelineName: "demo",
		Success:      true,
		Metrics:      map[string]any{"source": map[string]any{"files": 3}},
		Extras:       map[string]any{"fetched_path": "/tmp/x", "blob": []byte("huge")},
	}
	summary := r.Summary()
	extras, _ := summary["extras"].(map[string]any)
	if _, present := extras["blob"]; present {
		t.Error("byte payloads must not be dumped into a summary")
	}
	if extras["fetched_path"] != "/tmp/x" {
		t.Errorf("fetched_path = %v", extras["fetched_path"])
	}
	if summary["error"] != "" {
		t.Errorf("error = %v, want empty on success", summary["error"])
	}
}

func TestSlugify(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"emoji-sets:twemoji@17.0.0", "emoji-sets-twemoji-17-0-0"},
		{"icon-sets-flag@7.5.0", "icon-sets-flag-7-5-0"},
		{"  ", "pipeline"},
		{"!!!", "pipeline"},
		{"Already-Fine", "already-fine"},
	} {
		if got := slugify(tc.in); got != tc.want {
			t.Errorf("slugify(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestRecipePendingStringIsStable pins a literal another repository greps for.
// icon-sets-bundled's sync-upstream.yml turns this exact substring into a
// green run with a ::notice::; reword it and that scheduled job starts failing
// on a no-op it is meant to tolerate.
func TestRecipePendingStringIsStable(t *testing.T) {
	if !strings.Contains(ErrRecipePending.Error(), "recipe pending") {
		t.Fatalf("ErrRecipePending = %q, but icon-sets-bundled greps for \"recipe pending\"", ErrRecipePending)
	}
}

func TestContextConfigIsCopiedNotShared(t *testing.T) {
	shared := map[string]any{"pack": "ichava/icon-sets-flag"}
	ctx := newContext("demo", t.TempDir(), shared)
	ctx.Config["pack"] = "mutated"
	if shared["pack"] != "ichava/icon-sets-flag" {
		t.Fatal("a run mutated the builder's config map")
	}
}
