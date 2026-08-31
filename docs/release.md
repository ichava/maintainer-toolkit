[← Package README](../README.md#documentation)

# Release

*Reference.*

## Where this package is

`v0.1.0`, alongside every other package in the ecosystem.

The Docker image is published to GHCR and is **private**, which is why the pack workflows build from
a checkout of this repository rather than pulling it: another repository's `GITHUB_TOKEN` cannot pull
a private package.

## Cutting a release

1. `pytest`, the suite covers config loading, the checker's version comparison, the pipeline, and
   the vendored-version contract.
2. `ruff check` and `ruff format --check`.
3. Update `CHANGELOG.md`. The GitHub release body is that version's section; auto-generated notes
   are not a substitute.
4. Tag `vX.Y.Z` and push the tag.

## What a release affects

Nothing installs this package as a dependency, so a release here is not a consumer-facing event. It
matters to the pack workflows, which check this repository out at `main`, a break lands in every
pack's scheduled sync at once.

Pin the checkout to a tag in the pack workflows if that becomes uncomfortable. Floating on `main` is
tolerable while this is the maintainer's own tooling and the blast radius is a pull request nobody
has to merge.

## Versioning

Semver against the **config schema and the CLI**, the two things the pack repositories depend on.
Renaming a config key or a command is breaking. Adding a source type, a transform or a recipe is a
minor.

---

[← Docs index](../README.md#documentation)
