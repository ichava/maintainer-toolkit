"""GithubArchive source: download a release zipball/tarball + extract."""

from __future__ import annotations

import logging
import tarfile
import zipfile

from ichava_maintainer_toolkit.core.http import download
from ichava_maintainer_toolkit.core.pipeline import Source, StageContext
from ichava_maintainer_toolkit.core.progress import step

logger = logging.getLogger(__name__)


class GithubArchive(Source):
    """Pull a tagged GitHub release archive and extract under working_dir.

    Three convenience constructors are supported:

    1. Raw URL (use the `archive_url` kwarg, with optional `{version}` token).
    2. Owner+repo+version (auto-builds ``github.com/<owner>/<repo>/archive/refs/tags/v<version>.zip``).

    Args:
        archive_url: full URL template; `{version}` is interpolated.
        owner, repo, version: shorthand for the standard tag URL.
    """

    def __init__(
        self,
        *,
        archive_url: str | None = None,
        owner: str | None = None,
        repo: str | None = None,
        version: str | None = None,
    ) -> None:
        if not archive_url and not (owner and repo and version):
            raise ValueError(
                "GithubArchive needs either `archive_url` or all of `owner`/`repo`/`version`"
            )
        self._url_template = archive_url or (
            f"https://github.com/{owner}/{repo}/archive/refs/tags/v{{version}}.zip"
        )
        self._version = version
        self._owner = owner
        self._repo = repo

    def execute(self, ctx: StageContext) -> StageContext:
        if not self._version:
            raise ValueError(
                "GithubArchive: version is required (resolve via checker.latest_version)"
            )
        url = self._url_template.replace("{version}", self._version)

        target = ctx.working_dir / "gh-archive"
        target.mkdir(exist_ok=True)

        archive_path = target / "src.archive"
        with step(f"Downloading {url}"):
            download(url, archive_path)

        with step("Extracting archive"):
            # Try zip first (canonical), fall back to tar (some vendors only ship tarball).
            try:
                with zipfile.ZipFile(archive_path) as zf:
                    zf.extractall(target)
            except zipfile.BadZipFile:
                with tarfile.open(archive_path) as tf:
                    tf.extractall(target, filter="data")
            archive_path.unlink()

        # GitHub archives extract into <repo>-<version>/. Locate it.
        candidates = [p for p in target.iterdir() if p.is_dir() and p.name != "__MACOSX"]
        if len(candidates) != 1:
            raise RuntimeError(
                f"expected exactly one extracted root under {target}, got {[p.name for p in candidates]}"
            )
        extracted = candidates[0]

        ctx.extras["fetched_path"] = extracted
        ctx.extras["fetched_version"] = self._version
        ctx.extras["fetched_source"] = f"github-archive:{self._owner}/{self._repo}@v{self._version}"
        ctx.metrics.setdefault("source", {})["files"] = sum(
            1 for _ in extracted.rglob("*") if _.is_file()
        )
        return ctx
