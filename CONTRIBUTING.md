# Contributing

## Running it

```bash
pip install -e '.[dev]'
pytest
ruff check src tests && ruff format --check src tests
```

For an actual sync, use the Docker image. It is what CI runs, and a local Python with a different
Node or `gh` version is a different tool.

## Where things go

- A new **upstream shape** is a source strategy in `core/sources/`.
- A new **per-pack policy** is usually config, not code. Adding a recipe because a pack wants a
  different subdirectory means the config is missing a knob.
- A new **destination** is a sink in `core/sinks/`. Sink order matters: `Filesystem` writes assets,
  `VersionStamp` records the version, `GitBranch` commits. Stamping after the commit records a
  version that never shipped.

See [docs/architecture.md](docs/architecture.md) for why the parts are split that way.

## The rule that is easy to get wrong

**The vendored version belongs in the pack repo, not this one.** Under Docker, `config/` is a mount
from the runner's throwaway checkout, deleted by the workflow's cleanup step. Writing the version
there is how this tool spent weeks re-opening a pull request it had already opened.

## Conventions

- Conventional Commits. Subject 72 characters or fewer, imperative mood. The body explains why.
- No AI attribution anywhere: not in commits, PR titles or bodies, code comments or docs.
- No em-dashes in shipped docs or code comments.
- Type hints on public functions; the config schema is enforced by Pydantic at load time so a
  malformed config fails before anything reaches an upstream registry.

## Security

See [`SECURITY.md`](SECURITY.md).
