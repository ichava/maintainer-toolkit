"""Slugify: rename codepoint filenames to canonical slugs.

For emoji packs the upstream filenames are codepoints (`1f600.svg`);
the Categorise transform already does the rename + reorganise. This
transform exists for non-emoji vendors that ship SVGs with weird
casing or whitespace in filenames (rare but happens).
"""

from __future__ import annotations

import logging
import re

from ichava_maintainer_toolkit.core.pipeline import StageContext, Transform

logger = logging.getLogger(__name__)


class Slugify(Transform):
    """Lowercase + hyphenate every SVG filename in the in-flight tree."""

    def execute(self, ctx: StageContext) -> StageContext:
        root = ctx.extras.get("fetched_path")
        if root is None:
            raise RuntimeError("Slugify: fetched_path not set; needs a Source upstream")

        renamed = 0
        for svg in root.rglob("*.svg"):
            new_name = re.sub(r"[^a-z0-9.]+", "-", svg.name.lower()).strip("-.") + (
                "" if svg.name.lower().endswith(".svg") else ".svg"
            )
            if new_name != svg.name:
                svg.rename(svg.with_name(new_name))
                renamed += 1

        ctx.metrics.setdefault("transforms", {})["slugify"] = {"renamed": renamed}
        logger.info("slugify: renamed %d files", renamed)
        return ctx
