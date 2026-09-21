// Package pipeline composes one source, zero or more transforms and one or
// more sinks, and walks them in order threading a shared context between them.
//
// Ported from core/pipeline.py. Two things differ deliberately, both noted at
// the point they occur: Python's Source/Transform/Sink were pure runtime
// markers and are real types here, and a Go error carries no exception class
// name, so the failing stage is recorded as its own field rather than being
// spliced into the message.
package pipeline

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
)

// Context is the state threaded through every stage of one run.
//
// Stages mutate Extras to hand data forward. The pipeline guarantees
// WorkingDir exists for the duration of the run and removes it afterwards when
// it created it -- so a sink must copy out of it, never move.
type Context struct {
	PipelineName string
	WorkingDir   string

	// Config is the overlay supplied by the builder's Config method. Stages
	// read each other's keys, so namespace anything ambiguous.
	Config map[string]any

	// Extras is free-form scratch space and the real coupling between stages:
	// a source drops its extracted tree under FetchedPath, and every transform
	// and sink picks it up from there. The well-known keys have typed accessors
	// below -- prefer those, because a misspelt string key fails at runtime and
	// an accessor fails at compile time.
	Extras map[string]any

	// Metrics is per-stage counters. The reporter reads this.
	Metrics map[string]any
}

// Well-known Extras keys. These are the contract between stages; the accessors
// below exist so a typo cannot silently produce a missing value.
const (
	KeyFetchedPath    = "fetched_path"
	KeyFetchedVersion = "fetched_version"
	KeyFetchedSource  = "fetched_source"
	KeyCLDRPath       = "cldr_path"
	KeyCLDRText       = "cldr_text"
	KeyCLDRVersion    = "cldr_version"
	KeyCLDRRecords    = "cldr_records"
	KeyFilesystemTgt  = "filesystem_target"
	KeyGitBranch      = "git_branch"
	KeyGitSHA         = "git_sha"
	KeyPRURL          = "pr_url"
)

// Well-known Config keys, supplied by the recipes.
const (
	ConfigPack       = "pack"
	ConfigPackRoot   = "pack_root"
	ConfigOldVersion = "old_version"
)

func newContext(name, workingDir string, config map[string]any) *Context {
	cloned := make(map[string]any, len(config))
	for k, v := range config {
		cloned[k] = v
	}
	return &Context{
		PipelineName: name,
		WorkingDir:   workingDir,
		Config:       cloned,
		Extras:       map[string]any{},
		Metrics:      map[string]any{},
	}
}

// String returns a string-valued extra, and whether it was present as one.
func (c *Context) String(key string) (string, bool) {
	v, ok := c.Extras[key].(string)
	return v, ok
}

// SetString records a string-valued extra.
func (c *Context) SetString(key, value string) { c.Extras[key] = value }

// FetchedPath is the extracted tree a source produced. Every transform and the
// filesystem sink read it; a stage that needs it and does not find it must
// fail rather than silently do nothing.
func (c *Context) FetchedPath() (string, bool) { return c.String(KeyFetchedPath) }

// SetFetchedPath records the extracted tree. SubsetTo rewrites it to a
// subdirectory, which is how a source's `source_path` is honoured.
func (c *Context) SetFetchedPath(path string) { c.SetString(KeyFetchedPath, path) }

// FetchedVersion is the upstream version a source actually retrieved. The
// version stamp and the git sink both key off it.
func (c *Context) FetchedVersion() (string, bool) { return c.String(KeyFetchedVersion) }

// SetFetchedVersion records the retrieved version.
func (c *Context) SetFetchedVersion(v string) { c.SetString(KeyFetchedVersion, v) }

// ConfigString returns a string-valued config key, or "" when absent. Callers
// that need to distinguish absent from empty should read Config directly.
func (c *Context) ConfigString(key string) string {
	v, _ := c.Config[key].(string)
	return v
}

// Metric records a per-stage counter under a namespaced key.
func (c *Context) Metric(stage string, values map[string]any) {
	c.Metrics[stage] = values
}

// Stage is one step. Implementations are a Source, a Transform or a Sink.
type Stage interface {
	Name() string
	Execute(ctx *Context) error
}

// Source fetches assets and drops them under Context.WorkingDir.
//
// Python made Source/Transform/Sink empty subclasses of Stage, which the
// runner then treated identically -- they bought nothing but documentation.
// Here they are distinct, so passing a sink where a source belongs does not
// compile. Embed the matching Kind struct to declare which one a stage is.
type Source interface {
	Stage
	isSource()
}

// Transform mutates the in-flight tree under Context.FetchedPath.
type Transform interface {
	Stage
	isTransform()
}

// Sink writes the in-flight tree to its destination.
type Sink interface {
	Stage
	isSink()
}

// SourceKind, TransformKind and SinkKind are embedded to declare a stage's role.
type SourceKind struct{}

func (SourceKind) isSource() {}

type TransformKind struct{}

func (TransformKind) isTransform() {}

type SinkKind struct{}

func (SinkKind) isSink() {}

// Result is what a run returns.
type Result struct {
	PipelineName string
	Success      bool
	Metrics      map[string]any
	Extras       map[string]any

	// Err is the failure, or nil. Python formatted this as
	// "RuntimeError: boom"; Go errors carry no class name, so the message
	// stands alone and the stage that raised it is recorded separately.
	Err error

	// FailedStage names the stage that returned Err, empty on success.
	FailedStage string
}

// Error returns the failure as a string, or "" on success. It is the direct
// equivalent of Python's PipelineResult.error.
func (r Result) Error() string {
	if r.Err == nil {
		return ""
	}
	if r.FailedStage != "" {
		return r.FailedStage + ": " + r.Err.Error()
	}
	return r.Err.Error()
}

// Summary is the map the CLI prints. Byte payloads are dropped rather than
// dumped, matching Python.
func (r Result) Summary() map[string]any {
	extras := make(map[string]any, len(r.Extras))
	for k, v := range r.Extras {
		switch v.(type) {
		case []byte:
			continue
		}
		extras[k] = v
	}
	return map[string]any{
		"pipeline": r.PipelineName,
		"success":  r.Success,
		"metrics":  r.Metrics,
		"extras":   extras,
		"error":    r.Error(),
	}
}

// Pipeline is the fluent builder and runner.
type Pipeline struct {
	name       string
	source     Source
	transforms []Transform
	sinks      []Sink
	workingDir string
	config     map[string]any
	log        *slog.Logger

	// buildErr defers a builder misuse to Run, so the fluent chain stays
	// unbroken. Python raised immediately; a Go builder cannot without either
	// panicking or returning an error from every method, and both make the
	// recipes unreadable.
	buildErr error
}

// Named starts a pipeline.
func Named(name string) *Pipeline {
	return &Pipeline{name: name, config: map[string]any{}, log: slog.Default()}
}

// Source sets the single source. Setting it twice is an error, surfaced by Run.
func (p *Pipeline) Source(s Source) *Pipeline {
	if p.source != nil {
		p.setBuildErr(fmt.Errorf("pipeline %q already has a source set; only one source per run", p.name))
		return p
	}
	p.source = s
	return p
}

// Transform appends a transform. Any number is valid.
func (p *Pipeline) Transform(t Transform) *Pipeline {
	p.transforms = append(p.transforms, t)
	return p
}

// Sink appends a sink. Any number is valid; the order is load-bearing --
// filesystem, then version stamp, then git branch, so the version that is
// committed is the version that shipped.
func (p *Pipeline) Sink(s Sink) *Pipeline {
	p.sinks = append(p.sinks, s)
	return p
}

// WorkingDir overrides the per-run temporary directory. A caller-supplied
// directory is left in place afterwards; one the pipeline created is removed.
func (p *Pipeline) WorkingDir(path string) *Pipeline {
	p.workingDir = path
	return p
}

// Config merges keys any stage can read through Context.Config.
func (p *Pipeline) Config(kv map[string]any) *Pipeline {
	for k, v := range kv {
		p.config[k] = v
	}
	return p
}

// Logger replaces the logger. The TUI swaps in a handler that routes stage
// lines into the run view instead of stderr.
func (p *Pipeline) Logger(l *slog.Logger) *Pipeline {
	if l != nil {
		p.log = l
	}
	return p
}

// Name returns the pipeline's name.
func (p *Pipeline) Name() string { return p.name }

// Stages returns the stages in execution order. The TUI reads this to draw a
// row per stage before the run starts, so a pending stage is visible rather
// than appearing only once it begins.
func (p *Pipeline) Stages() []Stage {
	stages := make([]Stage, 0, 1+len(p.transforms)+len(p.sinks))
	if p.source != nil {
		stages = append(stages, p.source)
	}
	for _, t := range p.transforms {
		stages = append(stages, t)
	}
	for _, s := range p.sinks {
		stages = append(stages, s)
	}
	return stages
}

func (p *Pipeline) setBuildErr(err error) {
	if p.buildErr == nil {
		p.buildErr = err
	}
}

// Run executes the stages in order and returns the result.
//
// A stage failure stops the run -- the remaining stages do not execute, which
// is what keeps a sink from committing assets a transform failed to produce.
func (p *Pipeline) Run() (Result, error) {
	if p.buildErr != nil {
		return Result{PipelineName: p.name}, p.buildErr
	}
	if p.source == nil {
		return Result{PipelineName: p.name}, fmt.Errorf("pipeline %q has no source -- nothing to fetch", p.name)
	}
	if len(p.sinks) == 0 {
		return Result{PipelineName: p.name}, fmt.Errorf("pipeline %q has no sinks -- nothing to do with the data", p.name)
	}

	workingDir := p.workingDir
	createdTempDir := workingDir == ""
	if createdTempDir {
		dir, err := os.MkdirTemp("", "ichava-maintainer-toolkit-"+slugify(p.name)+"-")
		if err != nil {
			return Result{PipelineName: p.name}, err
		}
		workingDir = dir
	}
	if err := os.MkdirAll(workingDir, 0o755); err != nil {
		return Result{PipelineName: p.name}, err
	}
	if createdTempDir {
		// Only clean up what we created. A caller-supplied --working-dir is
		// theirs; removing it would delete a directory they may be inspecting.
		defer func() { _ = os.RemoveAll(workingDir) }()
	}

	ctx := newContext(p.name, workingDir, p.config)
	p.log.Info("pipeline starting", "pipeline", p.name, "working_dir", workingDir)

	for _, stage := range p.Stages() {
		p.log.Info("stage", "pipeline", p.name, "stage", stage.Name())
		if err := stage.Execute(ctx); err != nil {
			p.log.Error("pipeline failed", "pipeline", p.name, "stage", stage.Name(), "error", err)
			return Result{
				PipelineName: p.name,
				Success:      false,
				Metrics:      ctx.Metrics,
				Extras:       ctx.Extras,
				Err:          err,
				FailedStage:  stage.Name(),
			}, nil
		}
	}

	return Result{
		PipelineName: p.name,
		Success:      true,
		Metrics:      ctx.Metrics,
		Extras:       ctx.Extras,
	}, nil
}

// ErrRecipePending is returned for a pack whose recipe has not been written.
//
// The string is a contract, not a message. icon-sets-bundled's
// sync-upstream.yml greps the run output for "recipe pending" and converts the
// failure into a green run with a ::notice::, because that pack's `iconify`
// recipe genuinely does not exist yet. Change the wording and that workflow
// starts failing a scheduled job that is meant to be a documented no-op.
var ErrRecipePending = errors.New("recipe pending")

// slugify lowercases and hyphenates, for the temporary directory prefix.
func slugify(name string) string {
	var b strings.Builder
	lastHyphen := false
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastHyphen = false
		} else if !lastHyphen {
			b.WriteByte('-')
			lastHyphen = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "pipeline"
	}
	return out
}
