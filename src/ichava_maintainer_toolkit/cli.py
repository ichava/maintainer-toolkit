"""Typer CLI + questionary-driven interactive menu.

Subcommands:

* ``ichava-maintainer-toolkit menu``    -- interactive menu (default action via Docker CMD)
* ``ichava-maintainer-toolkit check``   -- poll upstreams; print a status table
* ``ichava-maintainer-toolkit sync``    -- run a recipe (refresh + commit + open PR)
* ``ichava-maintainer-toolkit recipe``  -- run a named recipe directly (skip the dispatcher)
* ``ichava-maintainer-toolkit list``    -- list registered packs

All long-running commands accept ``--dry-run`` and ``--config-dir``.
"""

from __future__ import annotations

import logging
from pathlib import Path
from typing import Annotated

import questionary
import typer
from rich.console import Console

from ichava_maintainer_toolkit import recipes
from ichava_maintainer_toolkit.core.checker import check_pack
from ichava_maintainer_toolkit.core.config import (
    DEFAULT_CONFIG_DIR,
    PackConfig,
    load_all,
    load_pack,
    load_registry,
    write_pack_config,
)
from ichava_maintainer_toolkit.core.reporters import TtyReporter
from ichava_maintainer_toolkit.version import __version__

app = typer.Typer(
    name="ichava-maintainer-toolkit",
    help="Maintainer toolkit for the Ichava icon-pack ecosystem.",
    no_args_is_help=False,
    add_completion=False,
)

console = Console()


def _setup_logging(verbose: bool) -> None:
    logging.basicConfig(
        level=logging.DEBUG if verbose else logging.INFO,
        format="%(asctime)s %(levelname)-7s %(name)s :: %(message)s",
        datefmt="%H:%M:%S",
    )


@app.callback()
def root(
    verbose: Annotated[bool, typer.Option("--verbose", "-v", help="Debug logging")] = False,
) -> None:
    _setup_logging(verbose)


@app.command()
def list_packs(
    config_dir: Annotated[
        Path, typer.Option("--config-dir", help="Where pack configs live")
    ] = DEFAULT_CONFIG_DIR,
) -> None:
    """List packs registered in `config/packs.json`."""
    registry = load_registry(config_dir)
    console.print(f"[bold]Registered packs ({len(registry.packs)}):[/bold]")
    for slug in registry.packs:
        try:
            cfg = load_pack(slug, config_dir)
            console.print(
                f"  • [cyan]{slug}[/cyan] -> {cfg.pack} (current: {cfg.current_version or '?'})"
            )
        except Exception as e:
            console.print(f"  • [red]{slug}[/red] (config error: {e})")


@app.command()
def check(
    pack: Annotated[str | None, typer.Option("--pack", help="Restrict to one pack slug")] = None,
    config_dir: Annotated[Path, typer.Option("--config-dir")] = DEFAULT_CONFIG_DIR,
) -> None:
    """Poll upstreams; print a status table."""
    packs = [load_pack(pack, config_dir)] if pack else load_all(config_dir)
    results = [check_pack(p) for p in packs]
    TtyReporter().render_check(results)
    raise typer.Exit(code=1 if any(r.stale for r in results) else 0)


@app.command()
def sync(
    pack: Annotated[str | None, typer.Option("--pack", help="Sync one pack slug")] = None,
    all_packs: Annotated[bool, typer.Option("--all/--no-all", help="Sync every pack")] = False,
    dry_run: Annotated[
        bool, typer.Option("--dry-run/--commit", help="Skip git commit + PR")
    ] = False,
    force: Annotated[bool, typer.Option("--force", help="Refresh even when not stale")] = False,
    config_dir: Annotated[Path, typer.Option("--config-dir")] = DEFAULT_CONFIG_DIR,
) -> None:
    """Refresh upstream assets, bump current_version, optionally commit + PR."""
    if not pack and not all_packs:
        raise typer.BadParameter("pass --pack=<slug> or --all")

    packs = load_all(config_dir) if all_packs else [load_pack(pack, config_dir)]
    exit_code = 0
    for cfg in packs:
        result = check_pack(cfg)
        if not result.latest:
            console.print(f"[yellow]{cfg.pack}: {result.reason}[/yellow]")
            continue
        if not result.stale and not force:
            console.print(f"[green]{cfg.pack}: up-to-date ({result.current})[/green]")
            continue

        console.print(
            f"[bold]{cfg.pack}: refreshing from {result.current} -> {result.latest}[/bold]"
        )
        outcome = _run_recipe(cfg, version=result.latest, dry_run=dry_run)
        if not outcome.success:
            console.print(f"[red]{cfg.pack}: {outcome.error}[/red]")
            exit_code = 1
            continue
        if not dry_run:
            _bump_current_version(cfg, result.latest, config_dir)
        console.print(f"[green]{cfg.pack}: done -- {outcome.summary()}[/green]")

    raise typer.Exit(code=exit_code)


@app.command()
def recipe(
    name: Annotated[str, typer.Argument(help="Recipe / pack slug to run")],
    version: Annotated[str | None, typer.Option("--version", help="Version override")] = None,
    dry_run: Annotated[bool, typer.Option("--dry-run/--commit")] = False,
    config_dir: Annotated[Path, typer.Option("--config-dir")] = DEFAULT_CONFIG_DIR,
) -> None:
    """Run a recipe directly (skip the upstream check). Useful for re-runs."""
    cfg = load_pack(name, config_dir)
    resolved_version = version or cfg.current_version
    if not resolved_version:
        raise typer.BadParameter(
            f"{cfg.pack}: no version supplied and no current_version in config"
        )
    outcome = _run_recipe(cfg, version=resolved_version, dry_run=dry_run)
    console.print(outcome.summary())
    raise typer.Exit(code=0 if outcome.success else 1)


@app.command()
def menu(
    config_dir: Annotated[Path, typer.Option("--config-dir")] = DEFAULT_CONFIG_DIR,
) -> None:
    """Interactive menu (default action when running the docker image)."""
    while True:
        action = questionary.select(
            "ichava/maintainer-toolkit — what next?",
            choices=[
                "Check upstream status (all packs)",
                "Check upstream status (single pack)",
                "Sync one pack",
                "Sync all packs (dry-run)",
                "List registered packs",
                "Exit",
            ],
        ).ask()

        if action is None or action == "Exit":
            console.print("[dim]bye[/dim]")
            return

        if action == "Check upstream status (all packs)":
            check(pack=None, config_dir=config_dir)
            continue

        if action.startswith("Check upstream status (single"):
            slug = _pick_pack(config_dir)
            if slug:
                check(pack=slug, config_dir=config_dir)
            continue

        if action == "Sync one pack":
            slug = _pick_pack(config_dir)
            if not slug:
                continue
            confirmed = questionary.confirm(
                f"Refresh {slug} from upstream + open a PR?", default=False
            ).ask()
            if confirmed:
                sync(pack=slug, all_packs=False, dry_run=False, force=False, config_dir=config_dir)
            continue

        if action == "Sync all packs (dry-run)":
            sync(pack=None, all_packs=True, dry_run=True, force=False, config_dir=config_dir)
            continue

        if action == "List registered packs":
            list_packs(config_dir=config_dir)
            continue


@app.command()
def version() -> None:
    """Print version and exit."""
    typer.echo(f"ichava-maintainer-toolkit {__version__}")


# ---------------------------------------------------------------------------
# helpers
# ---------------------------------------------------------------------------


def _pick_pack(config_dir: Path) -> str | None:
    registry = load_registry(config_dir)
    return questionary.select(
        "Pick a pack:",
        choices=[*registry.packs, "Cancel"],
    ).ask()


def _run_recipe(cfg: PackConfig, *, version: str, dry_run: bool) -> typing.Any:
    """Dispatch to the right recipe builder for this pack.

    Returns the last PipelineResult so callers can read its `.success`.
    Recipes that compose multiple pipelines (like emoji_sets) abort on
    first failure and surface that PipelineResult instead.

    Dispatcher table (kept tiny so adding a pack is one branch):

        cfg.name == "emoji-sets"     -> recipes.build_emoji_sets (multi-source)
        cfg.source.type == "npm"     -> recipes.build_simple_npm (~80% of packs)
        cfg.name == "bundled-icons"  -> NotImplementedError, see CHANGELOG
        anything else                -> NotImplementedError, write a recipe
    """
    if cfg.name == "emoji-sets":
        pipelines = recipes.build_emoji_sets(cfg, twemoji_version=version, dry_run=dry_run)
        last_result = None
        for p in pipelines:
            last_result = p.run()
            if not last_result.success:
                return last_result
        return last_result

    if cfg.source.type == "npm":
        return recipes.build_simple_npm(cfg, version=version, dry_run=dry_run).run()

    if cfg.name == "bundled-icons":
        raise typer.BadParameter(
            "bundled-icons recipe pending: this pack aggregates 70+ upstream Iconify sets "
            "via @iconify/json. Until "
            "`src/ichava_maintainer_toolkit/recipes/bundled_icons.py` lands, run "
            "upstream refreshes manually or pin to the vendored snapshot."
        )

    raise typer.BadParameter(
        f"{cfg.pack}: no recipe wired for source.type={cfg.source.type!r}. "
        f"Either add a `recipes/{cfg.name.replace('-', '_')}.py` module + dispatch here, "
        f"or switch the pack to source.type=npm if a single npm package covers it."
    )


def _bump_current_version(cfg: PackConfig, new_version: str, config_dir: Path) -> None:
    """Persist the bumped current_version back to the pack's config JSON."""
    cfg = cfg.model_copy(update={"current_version": new_version})
    write_pack_config(cfg, config_dir)


# Late-bound to dodge a circular import in some envs.
import typing  # noqa: E402

if __name__ == "__main__":
    app()
