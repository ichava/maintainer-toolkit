"""Pipeline mechanics: chaining, working_dir, error handling."""

from __future__ import annotations

from pathlib import Path

import pytest

from ichava_maintainer_toolkit.core.pipeline import Pipeline, Sink, Source, StageContext, Transform


class _RecordingSource(Source):
    def execute(self, ctx: StageContext) -> StageContext:
        d = ctx.working_dir / "rec"
        d.mkdir()
        (d / "a.svg").write_text("<svg/>", encoding="utf-8")
        ctx.extras["fetched_path"] = d
        ctx.metrics["source"] = {"files": 1}
        return ctx


class _RecordingTransform(Transform):
    def execute(self, ctx: StageContext) -> StageContext:
        # Add a marker so we can prove it ran.
        ctx.extras["transform_ran"] = True
        return ctx


class _RecordingSink(Sink):
    def __init__(self, target: Path) -> None:
        self.target = target
        self.fired = False

    def execute(self, ctx: StageContext) -> StageContext:
        self.fired = True
        # Copy a.svg into the recorded target to prove the path works.
        src = ctx.extras["fetched_path"] / "a.svg"
        self.target.write_text(src.read_text(encoding="utf-8"), encoding="utf-8")
        return ctx


def test_pipeline_runs_each_stage_in_order(tmp_path):
    sink = _RecordingSink(tmp_path / "out.svg")
    result = (
        Pipeline.named("test")
        .source(_RecordingSource())
        .transform(_RecordingTransform())
        .sink(sink)
        .run()
    )
    assert result.success
    assert sink.fired
    assert (tmp_path / "out.svg").read_text() == "<svg/>"


def test_pipeline_requires_a_source():
    with pytest.raises(ValueError, match="no source"):
        Pipeline.named("test").sink(_RecordingSink(Path("/nope"))).run()


def test_pipeline_requires_a_sink():
    with pytest.raises(ValueError, match="no sinks"):
        Pipeline.named("test").source(_RecordingSource()).run()


def test_pipeline_captures_stage_failure(tmp_path):
    class _Bomb(Transform):
        def execute(self, ctx: StageContext) -> StageContext:
            raise RuntimeError("boom")

    sink = _RecordingSink(tmp_path / "out.svg")
    result = Pipeline.named("test").source(_RecordingSource()).transform(_Bomb()).sink(sink).run()
    assert not result.success
    assert "boom" in (result.error or "")
    assert not sink.fired


def test_pipeline_double_source_raises():
    with pytest.raises(ValueError, match="already has a source"):
        Pipeline.named("test").source(_RecordingSource()).source(_RecordingSource())
