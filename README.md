# ichava/maintainer-toolkit

Maintainer toolkit for the Ichava Laravel icon-pack ecosystem.

A Docker-first Python tool that polls upstream registries (npm /
GitHub releases / GitHub tags), refreshes the bundled SVGs in each
pack repo, bumps `current_version`, commits to a branch, and opens a
PR. Replaces the per-pack `php artisan ichava:update*` command (which
couldn't persist anything because `vendor/` is regenerated on every
`composer install`).

End-user side of the same story: see
[`ichava/documentation/icon-pack-upstream-tracking.md`](https://github.com/ichava/documentation/blob/main/icon-pack-upstream-tracking.md).
This repo is the maintainer side.

## Install

```bash
git clone git@github.com:ichava/maintainer-toolkit.git
cd dev
make build      # builds the docker image
make help       # shows available targets
```

You don't need Python, Node, gh, or any system deps locally -- the
docker image bundles all of them. The image mounts the parent of this
repo at `/work` so it can read sibling pack repos.

## Common workflows

```bash
# Interactive menu (default Docker CMD)
make menu

# Check upstream status across every pack (no writes)
make check

# Sync one pack: pulls latest, refreshes assets, opens a PR
make sync PACK=tabler-icons

# Dry-run every pack (no commits)
make sync-all
```

## Configuration

Each pack has a JSON config under `config/`:

```
config/
├── packs.json                # the registry: which packs to manage
├── tabler-icons.json
├── flag-icons.json
├── bundled-icons.json
└── emoji-sets.json
```

A pack config:

```json
{
  "name": "tabler-icons",
  "pack": "ichava/tabler-icons",
  "pack_root": "/work/tabler-icons",
  "current_version": "3.0.0",
  "version_check_url": "https://registry.npmjs.org/@tabler/icons/latest",
  "source": {
    "type": "npm",
    "package": "@tabler/icons",
    "source_path": "icons"
  },
  "sinks": [
    {"type": "filesystem", "root": "{pack_root}/resources/assets/svg/files"},
    {"type": "git-branch", "repo_root": "{pack_root}", "branch": "chore/sync-upstream-{version}"}
  ]
}
```

`source.type` recognises: `npm`, `github-archive`, `github-release`,
`github-tag`, `script` (pack-supplied refresh recipe), `url`.

## <a name="documentation"></a>Documentation

- [Installation](docs/installation.md), Docker-first, and the local alternative
- [Getting started](docs/getting-started.md), check, sync, and what lands afterwards
- [Configuration](docs/configuration.md), the per-pack JSON and where the vendored version lives
- [Architecture](docs/architecture.md), the pipeline, and why the version records in the pack repo
- [Release](docs/release.md), cutting a version and what it affects

## Architecture

The tool is built around three composable primitives:

```
Source  (where SVGs come from)
  ↓
Transform (zero or more: sanitise, subset, slugify, categorise, indexer)
  ↓
Sink    (where SVGs land: filesystem, git-branch)
```

Fluent + chainable:

```python
from ichava_maintainer_toolkit import Pipeline, sources, transforms, sinks

(
    Pipeline.named("tabler-icons@3.44.0")
        .source(sources.NpmTarball("@tabler/icons", version="3.44.0"))
        .transform(transforms.SubsetTo("icons"))
        .transform(transforms.Sanitise())
        .sink(sinks.Filesystem(root="/work/tabler-icons/resources/assets/svg/files"))
        .sink(sinks.GitBranch(repo_root="/work/tabler-icons", branch="chore/sync-upstream-{version}"))
        .run()
)
```

Layout:

```
src/ichava_maintainer_toolkit/
├── cli.py                   typer + questionary interactive menu
├── core/
│   ├── pipeline.py          Pipeline / Stage / StageContext primitives
│   ├── config.py            JSON config loader (pydantic)
│   ├── http.py              tenacity-wrapped requests session
│   ├── git.py               git + gh-pr-create helpers
│   ├── checker.py           upstream version polling (PHP parity)
│   ├── sources/             NpmTarball, GithubArchive, UnicodeCldr
│   ├── transforms/          Sanitise, SubsetTo, Slugify, Categorise, Indexer
│   ├── sinks/               Filesystem, GitBranch
│   └── reporters/           rich-based TTY reporter
└── recipes/                 per-pack pre-baked pipelines
```

## Add a new pack

1. Create `config/<slug>.json` describing the upstream + sinks.
2. Add `<slug>` to `packs` in `config/packs.json`.
3. If the pack needs custom ETL (multi-source, CLDR taxonomy, etc.),
   write a recipe under `src/ichava_maintainer_toolkit/recipes/<slug>.py` and wire
   it into `cli._run_recipe`.
4. `make check-pack PACK=<slug>` to confirm the upstream poll resolves.

## Develop

```bash
make shell        # bash inside the container
make test         # pytest
make lint         # ruff check
make typecheck    # mypy strict
make format       # ruff format
```

Or, locally without Docker:

```bash
python -m venv .venv && source .venv/bin/activate
pip install -e ".[dev]"
ichava-maintainer-toolkit --help
```

## Status

**Alpha (v0.1.0).** Core primitives + `npm` strategy + `simple_npm`
recipe shipping. `script`-typed packs (emoji-sets, bundled-icons)
need their dedicated recipes ported from the legacy
`emoji-sets/scripts/build_emoji_assets.py`. See `CHANGELOG.md`.

## License

MIT. Copyright (c) Simtabi LLC.
