# Changelog

All notable changes to `ichava/maintainer-toolkit` follow [Keep a Changelog](https://keepachangelog.com/en/1.0.0/) and [Semantic Versioning](https://semver.org/).

## [0.1.1] - 2026-09-03

### Added

- `svg_policy` and `svg_filter`: the reader for the shared `svg-policy.json` and the pure
  policy enforcement it drives. Split so the rules can be tested without running a pipeline.
- `.scripts/sync-svg-policy.mjs`, the cross-repository drift gate for the vendored policy
  copies. A per-repo digest catches a local edit; only this sees that `core` has moved on.
- `.scripts/migration/policy.json` and the `census-before.json` / `census-after.json`
  baselines. `census.mjs` has always been able to compute what each sanitiser strips and
  never had a policy file to do it with, so it silently fell back to DISCOVERY.
- `lxml` as a dependency.

### Changed

- **`Sanitise` moved from regex to lxml.** `_EVENT_ATTR` matched double-quoted attributes
  only, so `<path onload='x()'/>` and every unquoted or entity-encoded spelling passed the
  build-time gate untouched, as did `<foreignObject>` and `<style>`. A regex cannot see
  where an attribute ends or that an entity expanded, so this was not fixable with a better
  pattern. `resolve_entities=False` and `no_network=True` now close XXE and billion-laughs
  structurally.
- `Sanitise` gains a `strict` mode that raises rather than cleans, for asserting a vendored
  tree is already clean in CI. Unparsable input is reported and left alone, never silently
  passed through.

### Fixed

- **The `emoji-sets` recipe pointed at upstream versions that do not exist**, which is why
  `ichava/emoji-sets` shipped a tagged package with zero SVGs (`V4`). `unicode_version`
  defaulted to `17.0` when Unicode has published nothing past `16.0`, so every run died on a
  404 before reaching npm; underneath that, `current_version` read `17.0.0` for
  `@twemoji/svg`, which has never been published. The three upstreams are independent and
  are now labelled as such.
- `UnicodeCldr` points a failed fetch at the Unicode directory listing instead of surfacing
  a bare `HTTPError`.

## [0.1.0] - 2026-08-31

First open-source release: the pack ingest pipelines, the upstream version checker, the
corpus census tool, and the CLI that drives them.

> Recorded retroactively on 2026-09-03. This file was a three-line stub when `v0.1.0` was
> tagged, so that release shipped with no notes -- which the org convention requires. The
> summary above is derived from the tagged tree, not from a contemporaneous record.
