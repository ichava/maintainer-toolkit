"""Sanitise SVG files in the in-flight tree.

Strips inline event handlers, `<script>` blocks, and `javascript:` URIs
that occasionally slip into upstream icon dumps. The host packs run
their own SvgSanitizer at runtime, but pruning here means we never
commit a hostile blob.
"""

from __future__ import annotations

import logging
import re

from ichava_maintainer_toolkit.core.pipeline import StageContext, Transform
from ichava_maintainer_toolkit.core.progress import file_progress

logger = logging.getLogger(__name__)

_SCRIPT_TAG = re.compile(r"<script\b[^>]*>.*?</script\s*>", re.IGNORECASE | re.DOTALL)
_EVENT_ATTR = re.compile(r'\s+on[a-z]+\s*=\s*"[^"]*"', re.IGNORECASE)
_JS_URI = re.compile(r'(href|xlink:href|src)\s*=\s*"javascript:[^"]*"', re.IGNORECASE)


class Sanitise(Transform):
    """Strip script tags, event handlers, javascript: URIs.

    Args:
        also_strip_class: tabler-icons embeds `class="icon icon-tabler ..."`
            that collides with host CSS; setting True drops them.
    """

    def __init__(self, *, also_strip_class: bool = False) -> None:
        self.also_strip_class = also_strip_class

    def execute(self, ctx: StageContext) -> StageContext:
        root = ctx.extras.get("fetched_path")
        if root is None:
            raise RuntimeError("Sanitise: fetched_path not set; needs a Source upstream")

        # Materialise the list so the progress bar can show a total + ETA.
        files = [svg for svg in root.rglob("*.svg")]
        cleaned = 0
        with file_progress(f"Sanitising {len(files)} SVGs", total=len(files)) as advance:
            for svg in files:
                content = svg.read_text(encoding="utf-8", errors="replace")
                new = _SCRIPT_TAG.sub("", content)
                new = _EVENT_ATTR.sub("", new)
                new = _JS_URI.sub("", new)
                if self.also_strip_class:
                    new = re.sub(r'\s+class\s*=\s*"[^"]*"', "", new, flags=re.IGNORECASE)
                if new != content:
                    svg.write_text(new, encoding="utf-8")
                    cleaned += 1
                advance()

        ctx.metrics.setdefault("transforms", {})["sanitise"] = {
            "cleaned": cleaned,
            "scanned": len(files),
        }
        logger.info("sanitise: cleaned %d / %d files", cleaned, len(files))
        return ctx
