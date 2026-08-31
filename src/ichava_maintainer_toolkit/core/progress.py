"""Shared progress / spinner helpers (rich-backed).

Every Source / Transform / Sink that does meaningful work should
surface progress through these helpers rather than rolling its own.
Centralising the styling keeps the terminal output consistent across
recipes and lets us swap the implementation (e.g. for CI-friendly
JSON line output) without touching call sites.

Usage::

    from ichava_maintainer_toolkit.core.progress import spinner, file_progress

    with spinner("Downloading @twemoji/svg@17.0.0"):
        # blocking work...

    with file_progress("Sanitising", total=3500) as advance:
        for f in files:
            sanitise(f)
            advance()

Behaviour:

* TTY runs render animated bars + ETA.
* Non-TTY runs (CI, redirected output) emit plain `progress: 3500/3500`
  lines so logs stay readable.
* Set ICHAVA_DEV_NO_PROGRESS=1 to silence everything.
"""

from __future__ import annotations

import logging
import os
import sys
import time
from collections.abc import Callable, Iterator
from contextlib import contextmanager

from rich.console import Console
from rich.progress import (
    BarColumn,
    Progress,
    SpinnerColumn,
    TextColumn,
    TimeElapsedColumn,
    TimeRemainingColumn,
)

logger = logging.getLogger(__name__)

_console = Console()


def _is_silent() -> bool:
    return os.environ.get("ICHAVA_DEV_NO_PROGRESS") == "1"


def _is_tty() -> bool:
    return sys.stdout.isatty()


@contextmanager
def spinner(label: str) -> Iterator[None]:
    """Indefinite spinner for "we're doing one big thing, no item count" tasks.

    Falls back to a single log line in non-TTY environments so CI logs
    stay readable.
    """
    if _is_silent() or not _is_tty():
        logger.info("%s …", label)
        start = time.monotonic()
        yield
        logger.info("%s done (%.1fs)", label, time.monotonic() - start)
        return

    with Progress(
        SpinnerColumn(),
        TextColumn("[bold blue]{task.description}"),
        TimeElapsedColumn(),
        console=_console,
        transient=True,
    ) as progress:
        task = progress.add_task(label, total=None)
        try:
            yield
        finally:
            progress.update(task, completed=1)


@contextmanager
def file_progress(label: str, total: int) -> Iterator[Callable[[], None]]:
    """Determinate progress bar for "iterate N items" tasks.

    Yields an `advance()` callable to bump the bar by one. Pass it into
    a loop. In non-TTY mode we emit periodic "[label] N/total" log
    lines (every 10% of progress) instead of redrawing a bar.
    """
    if total <= 0:
        # Nothing to iterate -- no point in showing a bar.
        yield lambda: None
        return

    if _is_silent() or not _is_tty():
        counter = {"done": 0, "next_tick": max(1, total // 10)}

        def _advance() -> None:
            counter["done"] += 1
            if counter["done"] >= counter["next_tick"] or counter["done"] == total:
                logger.info("%s: %d/%d", label, counter["done"], total)
                counter["next_tick"] += max(1, total // 10)

        yield _advance
        return

    with Progress(
        TextColumn("[bold blue]{task.description}"),
        BarColumn(),
        TextColumn("[progress.percentage]{task.percentage:>3.0f}%"),
        TextColumn("({task.completed}/{task.total})"),
        TimeRemainingColumn(),
        TimeElapsedColumn(),
        console=_console,
    ) as progress:
        task = progress.add_task(label, total=total)

        def _advance() -> None:
            progress.advance(task)

        yield _advance


@contextmanager
def step(label: str) -> Iterator[None]:
    """One-shot "now doing X" announcer that prints duration on exit.

    Useful for stages that do real work but don't naturally have an
    item count (HTTP fetch, archive extraction).
    """
    start = time.monotonic()
    _console.print(f"[dim]>[/dim] {label}")
    try:
        yield
    finally:
        elapsed = time.monotonic() - start
        _console.print(f"[dim]done[/dim] {label} [dim]({elapsed:.1f}s)[/dim]")
