"""SubsetTo: narrow the in-flight tree to one subdirectory.

Most npm/github archives ship more than just SVGs. After fetching
``@tabler/icons@3.44.0`` we want ``icons/outline/`` and ``icons/filled/``,
not ``LICENSE`` + ``package.json`` + ``CHANGELOG.md``. This transform
walks down to a named subdir and rewrites ``ctx.extras['fetched_path']``
so downstream sinks ignore the cruft.
"""

from __future__ import annotations

import logging

from ichava_maintainer_toolkit.core.pipeline import StageContext, Transform

logger = logging.getLogger(__name__)


class SubsetTo(Transform):
    """Replace `fetched_path` with a subdirectory of itself.

    Args:
        subdir: relative path under the current ``fetched_path``.
            Empty string is a no-op (kept so configs can declare it).
    """

    def __init__(self, subdir: str) -> None:
        self.subdir = subdir.strip("/")

    def execute(self, ctx: StageContext) -> StageContext:
        if not self.subdir:
            return ctx

        root = ctx.extras.get("fetched_path")
        if root is None:
            raise RuntimeError("SubsetTo: fetched_path not set; needs a Source upstream")

        new_root = root / self.subdir
        if not new_root.is_dir():
            raise RuntimeError(f"SubsetTo: subdir {self.subdir!r} not found under {root}")

        ctx.extras["fetched_path"] = new_root
        logger.info("subset: %s -> %s", root, new_root)
        return ctx
