"""Rich-backed TTY reporter for human consumption."""

from __future__ import annotations

from collections.abc import Iterable

from rich.console import Console
from rich.table import Table

from ichava_maintainer_toolkit.core.checker import CheckResult


class TtyReporter:
    """Print check results / sync outcomes to the terminal."""

    def __init__(self) -> None:
        self.console = Console()

    def render_check(self, results: Iterable[CheckResult]) -> None:
        results = list(results)
        table = Table(title="Upstream check", show_lines=False)
        table.add_column("Package", style="bold")
        table.add_column("Current")
        table.add_column("Latest")
        table.add_column("Status")
        table.add_column("Reason")

        for r in results:
            status = (
                "[green]ok[/green]"
                if r.latest and not r.stale
                else (
                    "[yellow]update-available[/yellow]"
                    if r.stale and r.latest
                    else "[red]error[/red]"
                )
            )
            table.add_row(
                r.package,
                r.current or "-",
                r.latest or "-",
                status,
                r.reason or ("→ " + r.release_url if r.release_url else "-"),
            )

        self.console.print(table)
        stale = sum(1 for r in results if r.stale and r.latest)
        if stale:
            self.console.print(f"[yellow]{stale} pack(s) behind upstream.[/yellow]")
        else:
            self.console.print("[green]All packs up to date.[/green]")
