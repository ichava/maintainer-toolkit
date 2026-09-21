// Package gitx shells out to git and gh.
//
// Deliberately subprocesses rather than a Go git library: the sync runs inside
// a container whose credentials arrive as git config
// (http.https://github.com/.extraheader, set by the workflow), and pull
// requests are opened with gh. A library would have to reimplement both, and
// the workflow's own comments record what it cost to get that plumbing right.
package gitx

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// DefaultAuthor is the identity sync commits are attributed to.
const DefaultAuthor = "ichava-sync-bot <19682005+imanimanyara@users.noreply.github.com>"

// Runner executes git and gh. Swapped in tests.
type Runner interface {
	Run(ctx context.Context, dir string, env []string, name string, args ...string) (string, error)
}

// ExecRunner runs real subprocesses.
type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, dir string, env []string, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	if len(env) > 0 {
		cmd.Env = append(cmd.Environ(), env...)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr

	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = strings.TrimSpace(stdout.String())
		}
		return "", fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, detail)
	}
	return strings.TrimSpace(stdout.String()), nil
}

// Git wraps the repository operations the sync needs.
type Git struct {
	Run    Runner
	Author string
}

// New returns a Git over real subprocesses.
func New() *Git { return &Git{Run: ExecRunner{}, Author: DefaultAuthor} }

func (g *Git) runner() Runner {
	if g.Run == nil {
		return ExecRunner{}
	}
	return g.Run
}

func (g *Git) author() string {
	if g.Author == "" {
		return DefaultAuthor
	}
	return g.Author
}

// HasUncommittedChanges reports whether the tree differs from HEAD.
func (g *Git) HasUncommittedChanges(ctx context.Context, repo string) (bool, error) {
	out, err := g.runner().Run(ctx, repo, nil, "git", "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return out != "", nil
}

// CreateBranch creates or resets a branch to HEAD.
//
// -B rather than -b, so a re-run over a branch a previous run left behind
// succeeds instead of failing on "already exists". The sync pushes to one
// stable branch per pack so each run updates a single pull request.
func (g *Git) CreateBranch(ctx context.Context, repo, name string) error {
	_, err := g.runner().Run(ctx, repo, nil, "git", "checkout", "-B", name)
	return err
}

// CommitAll stages everything and commits, returning the new SHA.
func (g *Git) CommitAll(ctx context.Context, repo, subject, body string) (string, error) {
	if _, err := g.runner().Run(ctx, repo, nil, "git", "add", "-A"); err != nil {
		return "", err
	}

	message := subject
	if body != "" {
		message = subject + "\n\n" + body
	}

	// GitHub's email-privacy guard (GH007) rejects a push whose commits carry
	// a real address, and --author sets only the author. The committer comes
	// from the environment, so it has to be set too or the push is refused
	// after the work has already been done.
	name, email := splitAuthor(g.author())
	env := []string{"GIT_COMMITTER_NAME=" + name, "GIT_COMMITTER_EMAIL=" + email}

	if _, err := g.runner().Run(ctx, repo, env, "git", "commit",
		"--author="+g.author(), "-m", message); err != nil {
		return "", err
	}
	return g.runner().Run(ctx, repo, nil, "git", "rev-parse", "HEAD")
}

// splitAuthor parses "Name <email>".
//
// Python did this with two string slices and raised IndexError on anything
// unexpected. This falls back to the default rather than failing a sync that
// had otherwise succeeded, because the identity is configuration and a
// malformed one is not worth discarding a completed asset refresh over.
func splitAuthor(author string) (name, email string) {
	open := strings.Index(author, "<")
	close := strings.LastIndex(author, ">")
	if open < 0 || close < open {
		return author, ""
	}
	return strings.TrimSpace(author[:open]), strings.TrimSpace(author[open+1 : close])
}

// PushBranch pushes and sets upstream.
//
// --force-with-lease, because the branch is stable across runs and a later run
// legitimately replaces an earlier one's commit. The lease is what stops it
// overwriting a human's edit to the same branch.
func (g *Git) PushBranch(ctx context.Context, repo, branch string, forceWithLease bool) error {
	args := []string{"push", "-u", "origin", branch}
	if forceWithLease {
		args = append(args, "--force-with-lease")
	}
	_, err := g.runner().Run(ctx, repo, nil, "git", args...)
	return err
}

// PRForBranch returns an existing pull request URL for a branch, or "".
//
// Filtered to open pull requests. Python asked gh for whatever it had, which
// matched a *merged* one once the first sync landed -- so every later run
// reported "already open", skipped creation and exited 0 while the branch it
// had just pushed had nothing tracking it.
func (g *Git) PRForBranch(ctx context.Context, repo, branch string) string {
	out, err := g.runner().Run(ctx, repo, nil, "gh", "pr", "list",
		"--head", branch, "--state", "open", "--json", "url", "-q", ".[0].url")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// OpenPR opens a pull request, or returns the open one already on the branch.
func (g *Git) OpenPR(ctx context.Context, repo, branch, title, body, base string) (string, error) {
	if existing := g.PRForBranch(ctx, repo, branch); existing != "" {
		return existing, nil
	}
	if base == "" {
		base = "main"
	}
	return g.runner().Run(ctx, repo, nil, "gh", "pr", "create",
		"--title", title, "--body", body, "--base", base, "--head", branch)
}

// FetchBranch fetches one remote branch into its tracking ref.
//
// The sync pushes to a stable branch and relies on --force-with-lease, which
// compares against the remote-tracking ref. actions/checkout fetches only the
// default branch, so that ref does not exist on a runner and the lease has
// nothing to compare -- git then rejects the push as "stale info", which reads
// like a race and is not one.
func (g *Git) FetchBranch(ctx context.Context, repo, branch string) error {
	_, err := g.runner().Run(ctx, repo, nil, "git", "fetch", "origin",
		fmt.Sprintf("+refs/heads/%s:refs/remotes/origin/%s", branch, branch))
	return err
}
