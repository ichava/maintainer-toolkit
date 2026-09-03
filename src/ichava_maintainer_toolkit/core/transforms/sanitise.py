"""Sanitise SVG files in the in-flight tree, against the shared policy.

This is gate 1 of three: the build-time gate, the only one that runs before a
file is committed. The other two are ``SanitizesSvg`` in ``ichava/core`` and the
two TypeScript clients, and all four now read the same
``svg-policy.json``.

It used to be 61 lines of regex, and regex was the wrong tool in a way that was
invisible from inside it. ``_EVENT_ATTR`` was ``\\s+on[a-z]+\\s*=\\s*"[^"]*"`` --
double quotes only -- so ``<path onload='x()'/>`` and ``<path onload=x()/>``
passed the gate untouched, as did any entity-encoded payload, any
``<foreignObject>``, and any ``<style>`` block. The strictest-looking runtime was
the weakest, and it ran first.

lxml parses the document properly, so the questions a regex has to guess at --
where does this attribute end, is this inside a comment, is this really a tag --
are answered by a parser instead.
"""

from __future__ import annotations

import logging

from lxml import etree

from ichava_maintainer_toolkit.core.pipeline import StageContext, Transform
from ichava_maintainer_toolkit.core.progress import file_progress
from ichava_maintainer_toolkit.core.transforms.svg_filter import (
    SvgPolicyViolationError,
    sanitise_bytes,
)

logger = logging.getLogger(__name__)

__all__ = ["Sanitise", "SvgPolicyViolationError"]


class Sanitise(Transform):
    """Filter every SVG in the tree down to what the shared policy permits.

    Args:
        also_strip_class: tabler-icons embeds ``class="icon icon-tabler ..."``
            that collides with host CSS; setting True drops them. The policy
            allows ``class`` in general -- this is a per-pack cosmetic choice,
            not a security one.
        strict: raise instead of cleaning. Use in CI to assert a vendored tree is
            already clean; the default cleans in place, which is what the ingest
            pipelines want.
    """

    def __init__(self, *, also_strip_class: bool = False, strict: bool = False) -> None:
        self.also_strip_class = also_strip_class
        self.strict = strict

    def execute(self, ctx: StageContext) -> StageContext:
        root = ctx.extras.get("fetched_path")
        if root is None:
            raise RuntimeError("Sanitise: fetched_path not set; needs a Source upstream")

        files = list(root.rglob("*.svg"))
        cleaned = 0
        unparsable = 0
        violations: list[str] = []

        with file_progress(f"Sanitising {len(files)} SVGs", total=len(files)) as advance:
            for svg in files:
                original = svg.read_bytes()

                try:
                    new, removed = sanitise_bytes(
                        original, also_strip_class=self.also_strip_class
                    )
                except etree.XMLSyntaxError as exc:
                    # Left alone and reported, never silently passed through.
                    unparsable += 1
                    logger.warning("sanitise: cannot parse %s: %s", svg, exc)
                    advance()
                    continue

                if removed:
                    violations.append(f"{svg.name}: {', '.join(sorted(set(removed)))}")

                if new != original:
                    svg.write_bytes(new)
                    cleaned += 1

                advance()

        if self.strict and violations:
            raise SvgPolicyViolationError(
                f"{len(violations)} file(s) violate the SVG policy:\n  "
                + "\n  ".join(violations[:20])
            )

        ctx.metrics.setdefault("transforms", {})["sanitise"] = {
            "cleaned": cleaned,
            "scanned": len(files),
            "unparsable": unparsable,
            "violations": len(violations),
        }
        logger.info(
            "sanitise: cleaned %d / %d files (%d unparsable)", cleaned, len(files), unparsable
        )
        return ctx
