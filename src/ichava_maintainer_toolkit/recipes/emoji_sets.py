"""Recipe: emoji-sets (multi-source: Twemoji + OpenMoji + CLDR).

The legacy `emoji-sets/scripts/build_emoji_assets.py` shipped an
all-in-one ETL pipeline. This recipe composes the same flow out of
ichava-maintainer-toolkit primitives so adding another emoji vendor (Noto, Apple
fallback, whatever) is one extra `Pipeline()` and a config tweak --
not a fork.

The recipe runs three pipelines sequentially:

1. **CLDR ingest** -- fetch `emoji-test.txt`, parse it, populate the
   shared records list. Other pipelines reuse it via context.
2. **Twemoji** -- npm pack `@twemoji/svg@<version>`, categorise by
   CLDR group, write into `files/twemoji/<group>/<slug>.svg`.
3. **OpenMoji** -- download the color + black release ZIPs, categorise
   the same way under `files/openmoji-color/` and `files/openmoji-black/`.

Build the indexes (`codepoints.json` + `names.json`) once at the end.
"""

from __future__ import annotations

import logging
from pathlib import Path

from ichava_maintainer_toolkit.core.config import PackConfig
from ichava_maintainer_toolkit.core.pipeline import Pipeline
from ichava_maintainer_toolkit.core.sinks import Filesystem, GitBranch, VersionStamp
from ichava_maintainer_toolkit.core.sources import GithubArchive, NpmTarball, UnicodeCldr
from ichava_maintainer_toolkit.core.transforms import Categorise, Indexer, Sanitise, SubsetTo

logger = logging.getLogger(__name__)


def build(
    pack: PackConfig,
    *,
    twemoji_version: str,
    openmoji_version: str = "15.1.0",
    unicode_version: str = "16.0",
    dry_run: bool = False,
) -> list[Pipeline]:
    """Return the three pipelines in execution order.

    ``unicode_version`` is NOT derived from ``twemoji_version`` and the two move
    independently. This defaulted to "17.0" while Unicode had published nothing
    past 16.0, so every run died on a 404 fetching ``emoji-test.txt`` -- which is
    why ``ichava/emoji-sets`` shipped a tagged package with zero SVGs (`V4`). The
    CLI does not pass this argument, so the default is the effective value:
    check https://unicode.org/Public/emoji/ before bumping it.


    The CLI is expected to ``run()`` them sequentially; on first failure
    it should abort the rest (don't commit a half-refreshed asset tree).
    """
    pack_root = Path(pack.pack_root)
    files_root = pack_root / "resources" / "assets" / "svg" / "files"

    # Twemoji on npm publishes `assets/svg/<codepoint>.svg`; @twemoji/svg
    # only ships the SVG dir (no `assets/svg` parent) so SubsetTo is empty.
    twemoji = (
        Pipeline.named(f"emoji-sets:twemoji@{twemoji_version}")
        .config(pack=pack.pack, pack_root=str(pack_root))
        .source(NpmTarball("@twemoji/svg", version=twemoji_version))
        .transform(Sanitise(also_strip_class=True))
        .source  # type: ignore[attr-defined]
    )

    # Twemoji + the CLDR ingest must share a context for Categorise(by='cldr')
    # to work. We compose them as one chain rather than two pipelines.
    twemoji = (
        Pipeline.named(f"emoji-sets:twemoji@{twemoji_version}")
        .config(pack=pack.pack, pack_root=str(pack_root))
        .source(
            _CompoundSource(
                NpmTarball("@twemoji/svg", version=twemoji_version),
                UnicodeCldr(unicode_version=unicode_version),
            )
        )
        .transform(Sanitise(also_strip_class=True))
        .transform(Categorise(by="cldr"))
        .transform(Indexer(targets=["codepoints", "names"]))
        .sink(Filesystem(root=str(files_root / "twemoji")))
    )

    openmoji_color = (
        Pipeline.named(f"emoji-sets:openmoji-color@{openmoji_version}")
        .config(pack=pack.pack, pack_root=str(pack_root))
        .source(
            _CompoundSource(
                GithubArchive(
                    archive_url=(
                        "https://github.com/hfg-gmuend/openmoji/archive/refs/tags/{version}.zip"
                    ),
                    owner="hfg-gmuend",
                    repo="openmoji",
                    version=openmoji_version,
                ),
                UnicodeCldr(unicode_version=unicode_version),
            )
        )
        .transform(SubsetTo("color/svg"))
        .transform(Sanitise())
        .transform(Categorise(by="cldr"))
        .sink(Filesystem(root=str(files_root / "openmoji-color")))
    )

    openmoji_black = (
        Pipeline.named(f"emoji-sets:openmoji-black@{openmoji_version}")
        .config(pack=pack.pack, pack_root=str(pack_root))
        .source(
            _CompoundSource(
                GithubArchive(
                    archive_url=(
                        "https://github.com/hfg-gmuend/openmoji/archive/refs/tags/{version}.zip"
                    ),
                    owner="hfg-gmuend",
                    repo="openmoji",
                    version=openmoji_version,
                ),
                UnicodeCldr(unicode_version=unicode_version),
            )
        )
        .transform(SubsetTo("black/svg"))
        .transform(Sanitise())
        .transform(Categorise(by="cldr"))
        .sink(Filesystem(root=str(files_root / "openmoji-black")))
    )

    pipelines = [twemoji, openmoji_color, openmoji_black]

    if not dry_run:
        # One git-branch sink after all three pipelines have written.
        # The orchestrator dispatches that as a separate Pipeline at the
        # end -- it has no source, just a sink.
        final = (
            Pipeline.named("emoji-sets:commit")
            .config(
                pack=pack.pack,
                pack_root=str(pack_root),
                old_version=pack.current_version or "unknown",
            )
            .source(_NoOpSource())
            .sink(VersionStamp(pack=pack))
            .sink(GitBranch(repo_root=str(pack_root)))
        )
        pipelines.append(final)

    return pipelines


# ---------------------------------------------------------------------------
# Helper sources used only by this recipe
# ---------------------------------------------------------------------------

from ichava_maintainer_toolkit.core.pipeline import Source, StageContext  # noqa: E402


class _CompoundSource(Source):
    """Run two Source stages back-to-back, threading the same context.

    Used to combine an asset source (npm/github) with the CLDR text
    fetcher. The CLDR data lands in ``ctx.extras['cldr_text']`` and the
    asset path overrides whatever the first source wrote -- exactly the
    interleave Categorise(by='cldr') expects.
    """

    def __init__(self, asset_source: Source, cldr_source: Source) -> None:
        self.asset_source = asset_source
        self.cldr_source = cldr_source

    def execute(self, ctx: StageContext) -> StageContext:
        ctx = self.cldr_source.execute(ctx)
        ctx = self.asset_source.execute(ctx)
        return ctx


class _NoOpSource(Source):
    """A pipeline still needs a source even when there's no fetch step.

    Used for the trailing ``commit`` pipeline that only runs a GitBranch
    sink against state another pipeline left on disk.
    """

    def execute(self, ctx: StageContext) -> StageContext:
        return ctx
