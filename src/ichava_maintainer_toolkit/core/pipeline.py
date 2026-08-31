"""Fluent, chainable Pipeline primitive.

A Pipeline composes one Source, zero-or-more Transforms, and one-or-more
Sinks. Each Stage is a small ABC with a single hook; the Pipeline walks
them in order and threads a StageContext (the shared scratchpad)
between them.

Example::

    from ichava_maintainer_toolkit import Pipeline, sources, transforms, sinks

    result = (
        Pipeline.named("emoji-sets/twemoji")
            .source(sources.NpmTarball("@twemoji/svg", version="17.0.0"))
            .transform(transforms.Sanitise())
            .transform(transforms.Categorise(by="cldr"))
            .sink(sinks.Filesystem(root="/work/emoji-sets/resources/assets/svg/files/twemoji"))
            .sink(sinks.GitBranch(repo_root="/work/emoji-sets", branch="chore/sync-{version}"))
            .run()
    )

    print(result.summary())  # {'copied': 3500, 'committed': True, 'pr_url': '...'}

The fluent API is the **preferred entry point** for both maintainers
(`make sync PACK=...`) and CI; see `recipes/` for pre-baked pipelines.
"""

from __future__ import annotations

import logging
from abc import ABC, abstractmethod
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any

logger = logging.getLogger(__name__)


@dataclass
class StageContext:
    """Threaded state shared across all stages of one pipeline run.

    Stages mutate `extras` to hand data forward. The Pipeline guarantees
    `working_dir` exists for the duration of the run and cleans it up
    on exit; sinks should COPY out of it, not move.
    """

    pipeline_name: str
    working_dir: Path
    config: dict[str, Any] = field(default_factory=dict)
    """Stage-supplied config overlay; stages can read each other's keys
    but should namespace by stage class name to avoid collisions."""

    extras: dict[str, Any] = field(default_factory=dict)
    """Free-form scratch space. Sources usually drop the path to their
    extracted tree under ``extras['fetched_path']`` so transforms /
    sinks can pick it up."""

    metrics: dict[str, Any] = field(default_factory=dict)
    """Per-stage counters: copies, skips, errors. Reporter reads this."""


class Stage(ABC):
    """One step in a pipeline. Subclass into Source / Transform / Sink."""

    @abstractmethod
    def execute(self, ctx: StageContext) -> StageContext:  # pragma: no cover
        """Mutate or replace the context, then return it."""
        raise NotImplementedError

    @property
    def name(self) -> str:
        return self.__class__.__name__


class Source(Stage):
    """Fetches assets from somewhere and drops them in `working_dir`.

    Concrete sources must populate ``ctx.extras['fetched_path']``
    (a Path under `ctx.working_dir`) and ``ctx.metrics['source.<name>']``
    with at least a ``count`` of fetched files.
    """


class Transform(Stage):
    """Mutates the in-flight asset tree under `ctx.extras['fetched_path']`.

    Composes; chain as many Transforms as you need (slugify after
    sanitise after categorise…). A Transform that produces a derived
    artefact (e.g. an indexer building codepoints.json) writes under
    `ctx.extras` for downstream Sinks to pick up.
    """


class Sink(Stage):
    """Writes the in-flight tree to its final destination.

    Multiple sinks per pipeline are valid (e.g. Filesystem + GitBranch).
    Sinks should be idempotent and side-effect-only on success.
    """


@dataclass
class PipelineResult:
    """What the pipeline returns to the caller after `.run()`."""

    pipeline_name: str
    success: bool
    metrics: dict[str, Any]
    extras: dict[str, Any]
    error: str | None = None

    def summary(self) -> dict[str, Any]:
        return {
            "pipeline": self.pipeline_name,
            "success": self.success,
            "metrics": self.metrics,
            "extras": {
                # Don't dump huge payloads in summaries; just paths + flags.
                k: str(v) if isinstance(v, Path) else v
                for k, v in self.extras.items()
                if not isinstance(v, (bytes, bytearray))
            },
            "error": self.error,
        }


class Pipeline:
    """Fluent builder + runner for a chain of Stages.

    Construct via :meth:`Pipeline.named` (preferred) or directly --
    `name` is the only required argument.
    """

    def __init__(self, name: str) -> None:
        self._name = name
        self._source: Source | None = None
        self._transforms: list[Transform] = []
        self._sinks: list[Sink] = []
        self._working_dir: Path | None = None
        self._config: dict[str, Any] = {}

    # ----- builder -----

    @classmethod
    def named(cls, name: str) -> Pipeline:
        return cls(name)

    def source(self, source: Source) -> Pipeline:
        if self._source is not None:
            raise ValueError(
                f"Pipeline '{self._name}' already has a source set; only one source per run."
            )
        self._source = source
        return self

    def transform(self, transform: Transform) -> Pipeline:
        self._transforms.append(transform)
        return self

    def sink(self, sink: Sink) -> Pipeline:
        self._sinks.append(sink)
        return self

    def working_dir(self, path: Path | str) -> Pipeline:
        """Override the working-dir (defaults to a fresh tempdir per run)."""
        self._working_dir = Path(path)
        return self

    def config(self, **kv: Any) -> Pipeline:
        """Merge stage-level config that any Stage can read via `ctx.config`."""
        self._config.update(kv)
        return self

    # ----- runner -----

    def run(self) -> PipelineResult:
        if self._source is None:
            raise ValueError(f"Pipeline '{self._name}' has no source -- nothing to fetch.")
        if not self._sinks:
            raise ValueError(
                f"Pipeline '{self._name}' has no sinks -- nothing to do with the data."
            )

        import shutil
        import tempfile

        # `created_tempdir` tracks who owns the cleanup. If the caller
        # supplied --working-dir we leave it alone.
        created_tempdir = self._working_dir is None
        working_dir = (
            Path(tempfile.mkdtemp(prefix=f"ichava-maintainer-toolkit-{_slugify(self._name)}-"))
            if created_tempdir
            else self._working_dir
        )
        assert working_dir is not None  # mypy: narrowed by branch above
        working_dir.mkdir(parents=True, exist_ok=True)

        ctx = StageContext(
            pipeline_name=self._name,
            working_dir=working_dir,
            config=dict(self._config),
        )

        logger.info("pipeline %s starting in %s", self._name, working_dir)
        try:
            for stage in [self._source, *self._transforms, *self._sinks]:
                logger.info("  stage: %s", stage.name)
                ctx = stage.execute(ctx)
        except Exception as e:
            logger.exception("pipeline %s failed at stage %s", self._name, stage.name)
            return PipelineResult(
                pipeline_name=self._name,
                success=False,
                metrics=ctx.metrics,
                extras=ctx.extras,
                error=f"{type(e).__name__}: {e}",
            )
        finally:
            if created_tempdir:
                shutil.rmtree(working_dir, ignore_errors=True)

        return PipelineResult(
            pipeline_name=self._name,
            success=True,
            metrics=ctx.metrics,
            extras=ctx.extras,
        )


def _slugify(name: str) -> str:
    """Lowercase + replace non-alnum with hyphens. Used for tempdir prefixes."""
    import re

    return re.sub(r"[^a-z0-9]+", "-", name.lower()).strip("-") or "pipeline"
