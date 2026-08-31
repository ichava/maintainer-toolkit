"""NpmTarball source: ``npm pack <package>@<version>`` then extract."""

from __future__ import annotations

import logging
import subprocess
import tarfile

from ichava_maintainer_toolkit.core.pipeline import Source, StageContext
from ichava_maintainer_toolkit.core.progress import step

logger = logging.getLogger(__name__)


class NpmTarball(Source):
    """Run ``npm pack`` for a registry version, extract under working_dir.

    The extracted tree lands at ``<working_dir>/package/`` (npm pack's
    canonical layout) and the path is stored under
    ``ctx.extras['fetched_path']`` for downstream stages.

    Args:
        package: npm package id, e.g. ``"@twemoji/svg"`` or ``"flag-icons"``.
        version: exact version to pack (no semver ranges -- we want
                 reproducibility, not latest-floating). Use the result
                 of `core.checker.latest_version()` to pick this.

    Raises:
        RuntimeError: if `npm` isn't on PATH or the pack call fails.
    """

    def __init__(self, package: str, version: str) -> None:
        self.package = package
        self.version = version

    def execute(self, ctx: StageContext) -> StageContext:
        target = ctx.working_dir / "npm"
        target.mkdir(exist_ok=True)

        with step(f"npm pack {self.package}@{self.version}"):
            try:
                tarball = subprocess.run(
                    ["npm", "pack", f"{self.package}@{self.version}", "--silent"],
                    cwd=target,
                    check=True,
                    capture_output=True,
                    text=True,
                ).stdout.strip()
            except FileNotFoundError as e:
                raise RuntimeError(
                    "npm not on PATH; the ichava/maintainer-toolkit image installs it -- mount issue?"
                ) from e
            except subprocess.CalledProcessError as e:
                raise RuntimeError(f"npm pack failed: {e.stderr.strip()}") from e

        with step(f"Extracting {tarball}"), tarfile.open(target / tarball) as tar:
            tar.extractall(target, filter="data")

        extracted = target / "package"
        if not extracted.is_dir():
            raise RuntimeError(f"npm pack didn't produce expected `package/` dir under {target}")

        ctx.extras["fetched_path"] = extracted
        ctx.extras["fetched_version"] = self.version
        ctx.extras["fetched_source"] = f"npm:{self.package}@{self.version}"
        ctx.metrics.setdefault("source", {})["files"] = sum(
            1 for _ in extracted.rglob("*") if _.is_file()
        )
        return ctx
