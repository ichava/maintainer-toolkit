"""Filesystem sink: copy SVGs from the in-flight tree into a pack repo.

Mirrors the in-flight tree's structure exactly. By default, the
destination is wiped before the copy so removed-upstream files don't
linger; pass ``incremental=True`` to skip the wipe (additive only).
"""

from __future__ import annotations

import logging
import shutil
from pathlib import Path

from ichava_maintainer_toolkit.core.config import interpolate
from ichava_maintainer_toolkit.core.pipeline import Sink, StageContext
from ichava_maintainer_toolkit.core.progress import file_progress

logger = logging.getLogger(__name__)


class Filesystem(Sink):
    """Copy SVGs from working_dir to the pack repo.

    Args:
        root: destination directory. May contain ``{version}`` and any
              other tokens declared in ``ctx.config``.
        incremental: when True, leave existing files in place (only add
                     / overwrite changed ones). Default False = wipe + replace.
    """

    def __init__(self, root: str | Path, *, incremental: bool = False) -> None:
        self.root = str(root)
        self.incremental = incremental

    def execute(self, ctx: StageContext) -> StageContext:
        source = ctx.extras.get("fetched_path")
        if source is None:
            raise RuntimeError("Filesystem sink: fetched_path not set; needs a Source upstream")

        bindings = {
            "version": ctx.extras.get("fetched_version", ""),
            "pack_root": ctx.config.get("pack_root", ""),
        }
        target_root = Path(interpolate(self.root, **bindings)).resolve()

        if not self.incremental and target_root.exists():
            logger.info("filesystem: wiping %s", target_root)
            shutil.rmtree(target_root)
        target_root.mkdir(parents=True, exist_ok=True)

        files = [svg for svg in source.rglob("*.svg")]
        copied = 0
        with file_progress(
            f"Copying {len(files)} SVGs to {target_root.name}", total=len(files)
        ) as advance:
            for svg in files:
                rel = svg.relative_to(source)
                dest = target_root / rel
                dest.parent.mkdir(parents=True, exist_ok=True)
                shutil.copy2(svg, dest)
                copied += 1
                advance()

        ctx.metrics.setdefault("sinks", {})["filesystem"] = {
            "copied": copied,
            "target": str(target_root),
        }
        ctx.extras["filesystem_target"] = target_root
        logger.info("filesystem: wrote %d SVGs into %s", copied, target_root)
        return ctx
