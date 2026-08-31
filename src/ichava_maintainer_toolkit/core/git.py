"""Git + GitHub helpers for the GitBranch sink + author-side workflows.

We deliberately shell out to `git` and `gh` instead of using a library
like dulwich -- the maintainer environment always has both installed,
and shelling out matches the behaviour CI runs unattended.

Public surface:

    has_uncommitted_changes(repo_root) -> bool
    create_branch(repo_root, name)
    commit_all(repo_root, subject, body, author=None) -> sha
    push_branch(repo_root, branch, *, force_with_lease=True)
    open_pr(repo_root, branch, title, body, base="main") -> url
    pr_for_branch(repo_root, branch) -> url | None
"""

from __future__ import annotations

import logging
import os
import subprocess
from pathlib import Path

logger = logging.getLogger(__name__)

DEFAULT_AUTHOR = "ichava-sync-bot <19682005+imanimanyara@users.noreply.github.com>"


def _run(
    cmd: list[str], cwd: Path, *, check: bool = True, env: dict[str, str] | None = None
) -> str:
    logger.debug("$ %s  (cwd=%s)", " ".join(cmd), cwd)
    proc = subprocess.run(
        cmd,
        cwd=cwd,
        check=check,
        capture_output=True,
        text=True,
        env={**os.environ, **(env or {})} if env else None,
    )
    return proc.stdout.strip()


def has_uncommitted_changes(repo_root: Path) -> bool:
    return bool(_run(["git", "status", "--porcelain"], repo_root))


def create_branch(repo_root: Path, name: str) -> None:
    """Create-or-checkout the branch. Idempotent (-B reuses if it exists)."""
    _run(["git", "checkout", "-B", name], repo_root)


def commit_all(
    repo_root: Path,
    subject: str,
    body: str = "",
    *,
    author: str = DEFAULT_AUTHOR,
) -> str:
    """Stage everything + create a single commit. Returns the new SHA."""
    _run(["git", "add", "-A"], repo_root)
    message = subject if not body else f"{subject}\n\n{body}"
    # Set committer to match author so GitHub email-privacy guard (GH007) is happy.
    env = {
        "GIT_COMMITTER_NAME": author.split(" <", 1)[0],
        "GIT_COMMITTER_EMAIL": author.split("<", 1)[1].rstrip(">"),
    }
    _run(
        ["git", "commit", f"--author={author}", "-m", message],
        repo_root,
        env=env,
    )
    return _run(["git", "rev-parse", "HEAD"], repo_root)


def push_branch(repo_root: Path, branch: str, *, force_with_lease: bool = True) -> None:
    args = ["git", "push", "-u", "origin", branch]
    if force_with_lease:
        args.append("--force-with-lease")
    _run(args, repo_root)


def pr_for_branch(repo_root: Path, branch: str) -> str | None:
    """Return the URL of the open PR for the branch, or None if there isn't one."""
    out = _run(
        ["gh", "pr", "list", "--head", branch, "--json", "url", "-q", ".[0].url"],
        repo_root,
        check=False,
    )
    return out or None


def open_pr(
    repo_root: Path,
    branch: str,
    title: str,
    body: str,
    *,
    base: str = "main",
) -> str:
    """Open a PR via `gh pr create`. Returns the new PR URL.

    Idempotent: if a PR already exists for the branch we return it
    instead of trying (and failing) to open a duplicate.
    """
    existing = pr_for_branch(repo_root, branch)
    if existing:
        logger.info("PR already exists for %s: %s", branch, existing)
        return existing

    return _run(
        ["gh", "pr", "create", "--title", title, "--body", body, "--base", base, "--head", branch],
        repo_root,
    )
