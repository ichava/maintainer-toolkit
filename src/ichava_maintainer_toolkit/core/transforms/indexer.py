"""Indexer: build the codepoints.json + names.json indexes for emoji packs.

Reads the records produced by :class:`Categorise` (Unicode CLDR data)
and writes two indexes that runtime callers use to resolve slugs
without parsing the full asset tree.
"""

from __future__ import annotations

import json
import logging

from ichava_maintainer_toolkit.core.pipeline import StageContext, Transform

logger = logging.getLogger(__name__)


class Indexer(Transform):
    """Write `codepoints.json` and/or `names.json` next to the assets dir.

    Args:
        targets: subset of ``["codepoints", "names"]`` to write.
                 Default: both.
        out_dir_key: key under ``ctx.extras`` whose value is where the
                     indexes should land. Defaults to writing one level
                     above ``fetched_path``.
    """

    def __init__(
        self,
        *,
        targets: list[str] | None = None,
        out_dir_key: str | None = None,
    ) -> None:
        self.targets = targets or ["codepoints", "names"]
        self.out_dir_key = out_dir_key

    def execute(self, ctx: StageContext) -> StageContext:
        records = ctx.extras.get("cldr_records")
        if records is None:
            raise RuntimeError(
                "Indexer: cldr_records not set; Categorise(by='cldr') must run first"
            )

        if self.out_dir_key and self.out_dir_key in ctx.extras:
            out_dir = ctx.extras[self.out_dir_key]
        else:
            root = ctx.extras.get("fetched_path")
            if root is None:
                raise RuntimeError("Indexer: no out_dir_key, and fetched_path not set")
            out_dir = root.parent
        out_dir.mkdir(parents=True, exist_ok=True)

        if "codepoints" in self.targets:
            codepoints = {"-".join(cps): _key(grp, name) for cps, grp, name in records}
            (out_dir / "codepoints.json").write_text(
                json.dumps(codepoints, indent=2, sort_keys=True) + "\n",
                encoding="utf-8",
            )
            logger.info("indexer: wrote codepoints.json (%d entries)", len(codepoints))

        if "names" in self.targets:
            names = {
                _slugify(name): {
                    "codepoints": list(cps),
                    "category": _slugify_group(grp),
                    "name": name,
                }
                for cps, grp, name in records
            }
            (out_dir / "names.json").write_text(
                json.dumps(names, indent=2, sort_keys=True) + "\n",
                encoding="utf-8",
            )
            logger.info("indexer: wrote names.json (%d entries)", len(names))
        return ctx


def _key(group: str, name: str) -> str:
    return f"{_slugify_group(group)}/{_slugify(name)}"


def _slugify(name: str) -> str:
    import re

    return re.sub(r"[^a-z0-9]+", "-", name.lower()).strip("-")


def _slugify_group(group: str) -> str:
    return _slugify(group.replace("&", " "))
