# Changelog

All notable changes to `ichava/maintainer-toolkit` follow [Keep a Changelog](https://keepachangelog.com/en/1.0.0/) and [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

- **`.scripts/wt` — one git worktree per session, over one object store.** This estate is
  thirteen repositories and more than one agent session works in them at once. A working tree has
  a single HEAD and a single set of files, so `git checkout`, `rebase`, `reset` and `rm` in one
  session act on another's work.

  Four incidents in one afternoon came from that, and every mitigation reached for was
  per-incident: a half-finished `stubs/` move read as residue, an `rm`/`checkout` race that made
  a commit record eight reference edits and zero file moves, a `git rebase` that rewrote another
  session's branch because `rebase` takes no branch argument, and a conflict resolution filed
  under the wrong changelog heading.

  ```bash
  export WT_SESSION=<name-the-work>
  .scripts/wt icon-sets-flag       # create or reuse, prints the path
  .scripts/wt --list               # every worktree in the estate, by session
  .scripts/wt --remove <repo>
  ```

  Commits, branches, tags and the object store stay shared. A worktree's `.git` is 4 KB against
  the repository's 3.9 MB.

  **Four hazards are encoded rather than left to memory:**

  - **Detached at `origin/main` by default.** A named branch cannot be checked out in two
    worktrees at once, and taking one hostage from the main checkout is the surprise this exists
    to remove.
  - **It creates siblings a test reads off disk.** `wt icon-sets-package-scaffolder` also creates
    `icon-sets-flag`, because `StubEstateParityTest` resolves
    `dirname(__DIR__, 3) . '/icon-sets-flag'` — a lone scaffolder worktree skips 8 parity cases
    and stays green.
  - **`vendor/` and `node_modules/` are gitignored, so they are per-worktree**, and it says so on
    creation. Roughly 128 MB of vendor per PHP package and 300–360 MB of node_modules for
    `browser` and `react-browser`, so make worktrees for what you are touching.
  - **The estate root is found by walking up** for a directory holding `packages/` and `demos/`,
    not by counting levels from the script. `.scripts/sample-svg-archetypes.mjs` in
    `react-browser` hardcoded an absolute path into one machine's home directory and was broken
    three separate ways by a restructure nobody connected to it.

  > **In a worktree, `.git` is a file rather than a directory**, so a checkout test written as
  > `is_dir('.git')` answers false there. `StubEstateParityTest` already uses `file_exists()` and
  > is correct — I reimplemented that check with `is_dir()` in a throwaway probe and was one step
  > from filing a defect against working code. Read the predicate the code uses.

## [Unreleased]

### Changed

- **Every pack slug moved to the `icon-sets-` names, and the config *filenames* had to move with
  them.** `load_pack(slug)` reads `config/{slug}.json`, and each pack's `sync-upstream.yml` passes
  `--pack="$PACK_SLUG"` with `PACK_SLUG: ${{ github.event.repository.name }}`. The repositories
  were renamed, so that variable now reads `icon-sets-flag` where the file was `flag-icons.json`.

  **Nothing would have reported this until a cron fired.** `sync-upstream.yml` runs on a
  schedule, in a repository nobody is watching, and the failure would have been
  `config not found` long after the rename that caused it. The four files, their `name`, `pack`
  and `pack_root` fields, and `config/packs.json` all move together.

  `pack_root` is `/work/<slug>` because the workflow mounts `-v "$PWD/..:/work"` and the runner
  checks a repository out at a directory named after it, so that path is the repository name too.

- **`cli.py` dispatches recipes on `cfg.name`**, so `emoji-sets` and `bundled-icons` moved there
  as well. A config rename without this leaves the emoji recipe unreachable and the pack falling
  through to the generic path.

  | Was | Is |
  |---|---|
  | `config/tabler-icons.json` | `config/icon-sets-tabler.json` |
  | `config/flag-icons.json` | `config/icon-sets-flag.json` |
  | `config/bundled-icons.json` | `config/icon-sets-bundled.json` |
  | `config/emoji-sets.json` | `config/icon-sets-emoji.json` |

  **The upstream fields are deliberately unchanged**, and this is the trap the rename exists
  around: `source.package: "flag-icons"` and
  `version_check_url: "https://registry.npmjs.org/flag-icons/latest"` name **lipis/flag-icons on
  npm**, not our pack. Same for `@tabler/icons`, `@twemoji/svg` and `@iconify/json`. A blanket
  slug replace points the update checker at a package that does not exist and reports
  `unreachable` rather than failing. Each edited file is asserted to carry the same upstream
  references afterwards as before.

  `.scripts/migration/census*.json` keeps the old slugs: it is a dated measurement of the tree as
  it was, not configuration.

### Added

- **`actionlint` runs on every pull request.** Nothing validated the workflow files at all:
  `release.yml` triggers only on `push: tags`, so a broken workflow was first observed as a
  release that refused to start — after the decision to release had been made.

  A YAML parse is not a substitute, and that is the sharp part. `yaml.safe_load` accepts a
  duplicate key and silently keeps the last one, so a double-applied patch that left
  `continue-on-error:` twice on a single step validated clean and would have failed only at tag
  time. `actionlint` rejects what Actions rejects.

  Checked against the defect rather than assumed: injecting that duplicate key, a typo'd step
  key, and an `if:` referencing a property that does not exist are all caught, while
  `yaml.safe_load` still parses the first of them without complaint.

### Fixed

- **A failed SBOM download no longer takes the whole release down.** `release.yml` generates the
  SBOM before it publishes, and the Syft installer fetches its checksums from GitHub's
  release-asset CDN. On 2026-09-21 that answered `504` for about twenty minutes, failing the job
  four times *before* the publish step — so the tag existed with no release behind it, which is
  the drift the release table exists to catch, produced by the release machinery itself.

  Two changes. The step now retries once after 45 seconds, which covers a single transient `504`
  — the common case. And a second failure no longer fails the job: the release publishes without
  the asset and emits a `::warning::` naming the re-run.

  **The two failure states are not equally bad, and that asymmetry is the whole design.** A
  release missing an attachment is repaired by re-running this workflow, which re-attaches it. A
  tag with no release persists silently until a person notices. Preferring the recoverable one
  is worth the loss of "every release always carries an SBOM" as an absolute.

  `fail_on_unmatched_files: false` is now stated on the publish step. It is already the action's
  default, but the point of this change is that a missing SBOM must not fail the publish, so it
  should not rest on a default a future reader has to know.

## [0.1.2] - 2026-09-21

### Changed

- **`codecov/codecov-action` pinned to `303a32d` (v7.1.1).** Third-party actions are pinned to
  the commit SHA of their latest release, with the version in a trailing comment; Dependabot
  moves the SHA and the comment together. The pin exists to stop a mutable tag moving under us,
  not to freeze a version, so it tracks latest rather than being held back.

## [0.1.1] - 2026-09-16

### Added

- `release.yml`, and third-party GitHub Actions pinned to the commit SHA of their latest
  release. `codecov/codecov-action` moved 4 → 7 and `docker/login-action` 3 → 4; the workflow
  already used the `files:` spelling v5 introduced, so no input changes were needed.

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
