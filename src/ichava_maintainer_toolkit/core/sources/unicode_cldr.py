"""UnicodeCldr source: download and cache emoji-test.txt for a CLDR version.

Used by the emoji-sets recipe to drive the canonical
codepoint -> (group, subgroup, slug) mapping. Strictly speaking this
is an auxiliary data source, not an SVG source; the recipe combines
it with one of the other sources (NpmTarball / GithubArchive).
"""

from __future__ import annotations

import logging

from ichava_maintainer_toolkit.core.http import download
from ichava_maintainer_toolkit.core.pipeline import Source, StageContext

logger = logging.getLogger(__name__)


class UnicodeCldr(Source):
    """Fetch ``unicode.org/Public/emoji/<version>/emoji-test.txt`` to working_dir.

    Stores the path under ``ctx.extras['cldr_path']`` and the raw text
    under ``ctx.extras['cldr_text']`` so transforms downstream don't
    need to re-read the file.

    Args:
        unicode_version: the emoji-version segment (e.g. ``"17.0"``).
    """

    def __init__(self, unicode_version: str = "17.0") -> None:
        self.unicode_version = unicode_version

    def execute(self, ctx: StageContext) -> StageContext:
        # A wrong version here 404s, and the raw HTTPError does not say why.
        # Unicode publishes one directory per emoji release and nothing for a
        # version that has not shipped, so the usual cause is a default that ran
        # ahead of the standard -- exactly what left emoji-sets empty (`V4`).
        # Check https://unicode.org/Public/emoji/ for what exists.
        url = f"https://unicode.org/Public/emoji/{self.unicode_version}/emoji-test.txt"
        target = ctx.working_dir / f"emoji-test-{self.unicode_version}.txt"
        download(url, target)
        ctx.extras["cldr_path"] = target
        ctx.extras["cldr_text"] = target.read_text(encoding="utf-8")
        ctx.extras["cldr_version"] = self.unicode_version
        return ctx
