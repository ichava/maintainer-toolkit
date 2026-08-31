"""Categorise: re-arrange a flat asset tree into per-category subdirectories.

The canonical use case is the emoji-sets recipe -- upstream Twemoji /
OpenMoji ship one flat dir of `<codepoint>.svg` files; we group them
by Unicode CLDR group (smileys-emotion, animals-nature, ...) before
committing.

For non-emoji packs this transform is usually a no-op; pass
``by="passthrough"`` (the default).
"""

from __future__ import annotations

import logging
import re
import shutil

from ichava_maintainer_toolkit.core.pipeline import StageContext, Transform

logger = logging.getLogger(__name__)


class Categorise(Transform):
    """Group SVGs by an upstream taxonomy.

    Args:
        by: ``"cldr"`` (drives off ``ctx.extras['cldr_text']`` -- a
             Unicode emoji-test.txt loaded by UnicodeCldr source) or
             ``"passthrough"`` (no-op).
    """

    CLDR_GROUP_RE = re.compile(r"^# group:\s*(.+)$")
    CLDR_SUBGROUP_RE = re.compile(r"^# subgroup:\s*(.+)$")
    CLDR_LINE_RE = re.compile(
        r"^([0-9A-Fa-f][0-9A-Fa-f ]+);\s*fully-qualified\s*#\s*\S+\s+E\d+\.\d+\s+(.+)$"
    )

    def __init__(self, *, by: str = "passthrough") -> None:
        self.by = by

    def execute(self, ctx: StageContext) -> StageContext:
        if self.by == "passthrough":
            return ctx
        if self.by != "cldr":
            raise ValueError(f"Categorise: unknown taxonomy {self.by!r}")

        cldr_text = ctx.extras.get("cldr_text")
        if cldr_text is None:
            raise RuntimeError(
                "Categorise(by='cldr'): UnicodeCldr source must run first to populate cldr_text"
            )

        root = ctx.extras.get("fetched_path")
        if root is None:
            raise RuntimeError("Categorise: fetched_path not set; needs a Source upstream")

        records = list(self._parse(cldr_text))
        # Map codepoint-filename -> (group, slug)
        index: dict[str, tuple[str, str]] = {}
        for codepoints, group, name in records:
            slug = _slugify(name)
            index["-".join(codepoints).lower()] = (_slugify_group(group), slug)

        copied = 0
        missing = 0
        # Don't bulk-recurse; assume one-level flat upstream tree (Twemoji
        # / OpenMoji ship that way). For anything deeper, run a SubsetTo
        # first to narrow.
        files = [p for p in root.rglob("*.svg")]
        # Move all svgs into a staging dir, then dispatch into the new tree.
        staging = root.parent / (root.name + "__staging__")
        if staging.exists():
            shutil.rmtree(staging)
        staging.mkdir()
        for f in files:
            shutil.move(str(f), str(staging / f.name))
        # Wipe the original + rebuild structure.
        shutil.rmtree(root)
        root.mkdir()

        for f in staging.iterdir():
            stem = f.stem.lower()
            entry = index.get(stem)
            if entry is None:
                missing += 1
                continue
            group, slug = entry
            target = root / group / f"{slug}.svg"
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.move(str(f), str(target))
            copied += 1
        shutil.rmtree(staging, ignore_errors=True)

        ctx.metrics.setdefault("transforms", {})["categorise"] = {
            "copied": copied,
            "missing": missing,
        }
        # Hand the parsed records onward for the indexer.
        ctx.extras["cldr_records"] = records
        logger.info("categorise(cldr): copied %d, missing %d", copied, missing)
        return ctx

    @classmethod
    def _parse(cls, text: str):
        # subgroup is parsed (for completeness with the CLDR format) but
        # not yielded today; consumers only need codepoints + group + name.
        # Drop the `m = subgroup_re.match(line)` line if a future record
        # type needs it.
        group = ""
        for raw in text.splitlines():
            line = raw.strip()
            if not line:
                continue
            if line.startswith("#"):
                m = cls.CLDR_GROUP_RE.match(line)
                if m:
                    group = m.group(1).strip()
                continue
            m = cls.CLDR_LINE_RE.match(line)
            if not m:
                continue
            codepoints = m.group(1).split()
            name = m.group(2).strip()
            yield codepoints, group, name


def _slugify(name: str) -> str:
    return re.sub(r"[^a-z0-9]+", "-", name.lower()).strip("-")


def _slugify_group(group: str) -> str:
    # CLDR groups are like "Smileys & Emotion" -> "smileys-emotion"
    return _slugify(group.replace("&", " ").replace(" ", "-"))
