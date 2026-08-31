"""VersionStamp sink: record the fetched version inside the pack repo.

Runs after the Filesystem sinks and before GitBranch, so the version and the
assets it describes land in one commit.

Without this the sync cannot converge (V49). `current_version` used to live only
in this repository's ``config/<pack>.json``, and the bump was written through the
``/app/config`` mount -- the runner's throwaway checkout, deleted by the
workflow's cleanup step. The recorded version never moved, so every scheduled run
re-detected the same upstream release and re-opened the same pull request.
"""

from __future__ import annotations

import logging

from ichava_maintainer_toolkit.core.config import PackConfig, write_vendored_version
from ichava_maintainer_toolkit.core.pipeline import Sink, StageContext

logger = logging.getLogger(__name__)


class VersionStamp(Sink):
    """Write the fetched upstream version into the pack repo's own config.

    Args:
        pack: the pack whose `version_file` and `version_keys` say where the
            record lives.
    """

    def __init__(self, *, pack: PackConfig) -> None:
        self.pack = pack

    def execute(self, ctx: StageContext) -> StageContext:
        version = ctx.extras.get("fetched_version", "")
        if not version:
            logger.info("version-stamp: no fetched_version in context -- skipping")
            ctx.metrics.setdefault("sinks", {})["version_stamp"] = {"status": "no-version"}
            return ctx

        written = write_vendored_version(self.pack, version)
        if written is None:
            logger.warning(
                "version-stamp: %s has no %s -- the sync will re-detect %s next run",
                self.pack.pack,
                self.pack.version_file,
                version,
            )
            ctx.metrics.setdefault("sinks", {})["version_stamp"] = {"status": "no-file"}
            return ctx

        logger.info("version-stamp: recorded %s in %s", version, written)
        ctx.metrics.setdefault("sinks", {})["version_stamp"] = {
            "status": "stamped",
            "version": version,
            "file": str(written),
        }
        return ctx
