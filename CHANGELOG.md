# Changelog

All notable changes to `ichava/maintainer-toolkit` follow [Keep a Changelog](https://keepachangelog.com/en/1.0.0/) and [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

- **`SSL_CERT_FILE` is honoured, and the transport keeps its proxy.** Go reads that variable on
  Linux but not on macOS, where `crypto/x509` uses the platform verifier and
  `x509.SystemCertPool()` returns **zero subjects** however the variable is set. That costs
  nothing in production -- the image is Linux -- but it stops the tool working on a developer
  machine behind a TLS-intercepting proxy, which is an ordinary corporate setup and is also the
  sandbox this was built in. The bundle is now loaded into an explicit pool.

  The transport is *cloned* from `http.DefaultTransport` rather than built fresh, and that is the
  load-bearing half. A hand-rolled `&http.Transport{TLSClientConfig: …}` silently drops
  `Proxy: http.ProxyFromEnvironment`: every request then dials directly and fails as
  `no such host`, which reads like DNS rather than like the configuration mistake it is. Pinned
  by a test that asserts a bare `&http.Transport{}` has no Proxy and a clone does.

  With it, `check` runs against the live registries for the first time: tabler 3.46.0 behind
  3.47.0, flag 7.0.0 behind 7.5.0, bundled 2.2.0 behind 2.2.531, emoji current, exit 1. The
  release URLs, the staleness comparison and the CI exit contract are now verified against real
  registry responses rather than stubs.

- **`imt bundled audit` -- identifies, by measurement, where each vendored set in the aggregator
  pack came from.** It fetches candidate npm packages, scores every directory in them against the
  committed icons, and believes nothing on the strength of a name: a candidate is accepted only
  when the bytes it ships are the bytes already committed.

  It was written instead of the `iconify` recipe the config asks for, because that recipe would
  have been destructive. Measured across the pack, the 121,314 committed icons carry **26 distinct
  `<svg>` attribute signatures** -- `bootstrap-icons` keeps `class="bi bi-alarm"`, `heroicons`
  keeps `data-slot`, `fontawesome` keeps its licence comment, `akar-icons` is stroke-based -- where
  anything rendered from `@iconify-json/*` would have exactly one. Each set is its own upstream's
  native distribution, so `update_command.type: "iconify"` describes an approach this pack was
  never built with, and implementing it would have rewritten every file into a uniform dialect,
  discarding a licence attribution among much else.

  Refreshing the pack therefore needs a per-set manifest that does not exist. This builds one.
  Run against the real pack it resolves **21 of 72 sets**, two of them exactly:
  `bootstrap-icons@1.13.1` reproduces all 2,078 of its committed icons byte for byte, and
  `feather-icons@4.29.2` all of its.

  Confidence is graded on two signals kept deliberately apart, because the audit showed why:
  `ionicons` matched **1,356 of 1,356 names with zero identical bytes**. The committed files
  expand what upstream now writes as CSS classes into explicit `fill`/`stroke` attributes and use
  comma-separated path data -- an older release of the right project, not a wrong project. So byte
  identity grades the *version* and name coverage grades the *source*, and `package` means
  "upstream found, pin a version".

  For a set the name patterns cannot reach, supply the package and let the tool answer:

      imt bundled audit --set phosphor-icons --package @phosphor-icons/core

  which reports `partial 1512/9072 package/assets/bold` -- the project is right, and the pack has
  flattened several weight variants into one directory, so no single upstream directory covers it.

### Fixed

- **`config/icon-sets-bundled.json` describes a pack that does not exist.** Its `_status` points
  at a recipe path that was never written, and the pack's own `update_command._note` claims the
  sets listed under `metadata.data.categories.*.sets` "define what's vendored". They do not: 42
  sets are declared and 72 are vendored, 13 declared sets have no directory at all -- including
  `lucide` and `material-design-icons` -- and 43 vendored sets are undeclared. Recorded here
  rather than silently corrected, because which list is meant to be authoritative is a decision
  for the pack's owner.

- **The sources, transforms, sinks and recipes.** The engine is complete: `sync` and `recipe`
  now run real pipelines rather than reporting that they cannot.

  Verified end to end against a real upstream, not fixtures. A `recipe icon-sets-flag
  --version 7.5.0 --dry-run` fetches from npm, narrows to `flags/`, applies the policy to all
  542 icons and writes them into the pack -- and the result is **byte-for-byte identical to
  what a previous Python run committed**, all 542 files.

  That comparison found the one defect the corpus differential could not: lxml models the text
  after an element as that element's `.tail`, so `el.remove(child)` takes both, while a tree
  built from Go's decoder holds it as a separate sibling. Removing only the element left its
  indentation behind, and 4 of the 542 icons differed by exactly one orphaned newline. The
  corpus test compares surviving structure, and whitespace is not structure -- so it took a
  byte diff against a committed artefact to see it.

  Deliberate departures from the Python, each with a test:

  - **`icon-sets-emoji` was never version-stamped.** Its recipe ends with a commit-only
    pipeline whose source was a no-op, and every pipeline gets its own context -- so
    `VersionStamp` found no `fetched_version`, logged "skipping" and did nothing. The pack's
    vendored version never moved and its sync could not converge, which is exactly the V49
    failure the vendored-version design exists to prevent, still live in the one recipe that
    composes several pipelines. The commit pipeline now carries the version forward.
  - **Archive extraction refuses a traversing entry.** Python got that free from `tarfile`'s
    `filter="data"`; Go's `archive/tar` and `archive/zip` do nothing about it, and this fetches
    archives from the internet inside a container with the pack tree mounted.
  - **`gh pr list` is filtered to open pull requests.** Unfiltered, it matched the *merged* one
    once the first sync landed, so every later run printed "already open", skipped creation and
    exited 0 while the branch it had just pushed had nothing tracking it.
  - **The sync fetches the remote tracking ref before pushing.** `actions/checkout` fetches only
    the default branch, so `refs/remotes/origin/<branch>` does not exist on a runner and
    `--force-with-lease` has nothing to compare -- git then rejects the push as "stale info",
    which reads like a race and is not one.
  - **`Categorise` refuses a taxonomy that parsed to zero records** rather than filing every
    icon under "missing" and silently emptying the pack.

- **`internal/core/svg` -- the SVG policy layer, the riskiest part of the port.** lxml is a real
  DOM and Go's `encoding/xml` is a streaming decoder, so the two had to be made to agree rather
  than assumed to. It is verified by running **both implementations over all 17,812 vendored
  icons** in the four pack checkouts and requiring identical answers for what the policy removed
  and what structure survived: **zero mismatches**, in ten seconds.

  That gate earned itself immediately. A fixture suite would have passed while two real defects
  sat in the port:

  - **261 metronic flags declare `encoding="iso-8859-1"`.** Go's decoder refuses any encoding it
    has no table for; lxml has them all. There is now a charset reader for Latin-1 -- the only
    non-UTF-8 encoding the corpus contains, measured -- and an explicit error for anything else,
    because silently decoding an unknown encoding as Latin-1 mangles multi-byte characters into
    pairs of accented ones and the damage is invisible until someone renders a title.
  - **`styleAttribute` in the policy is an object, not a boolean.** Python reads it with a
    truthiness test. Typing it as a Go `bool` made the whole policy fail to load -- and had that
    failure been swallowed, `style` would have dropped off the merged allow-list, removing the
    sole paint source from 261 of metronic's 501 icons. That is the exact trap the policy file's
    own comments warn about.

  One Go-specific hazard has no Python counterpart and is pinned by its own test: lxml keeps
  namespace declarations out of `el.attrib` entirely, while Go's decoder hands them over as
  ordinary attributes -- and `xmlns` is not on the allow-list, so filtering them would have
  stripped the SVG namespace from every icon in the estate.

  The vendored policy is registered in `.scripts/sync-svg-policy.mjs` as a fifth consumer and
  pinned by digest here, so the Go copy is covered by the same cross-repo gate as the other four.

  Worth a separate decision, not taken here: the policy allows all 22 `fe*` filter tags and none
  of their attributes, so `<feGaussianBlur stdDeviation="2"/>` survives as `<feGaussianBlur/>`
  and blurs by zero. Both implementations agree, so it is the policy's behaviour rather than a
  port defect -- but the canonical file lives in core and four runtimes read it.

- **The CLI and the TUI, over one engine.** `cmd/imt` builds a cobra tree with all six commands
  the Python declared -- `list-packs`, `check`, `sync`, `recipe`, `menu`, `version` -- and the
  exit codes they are consumed by: **1** when `check` finds anything stale, **1** when a `sync`
  recipe fails, **2** for a usage error. `sync` and `recipe` are wired but report that the
  recipes are not ported yet; Python is still the entrypoint and still does the real work.

  `menu` is now a Bubble Tea TUI -- menu, pack list, pack detail and a live check view sharing
  one spinner and one table. **In a non-TTY it prints help and exits 0.** The Python called
  `questionary.select()` unconditionally, and `menu` is the Docker image's default `CMD`, so on a
  runner it blocked on a stdin that never answered until the job hit its 30-minute timeout.

  The parity rule is structural rather than aspirational: `internal/app` holds the orchestration
  and `internal/ui` the rendering, and both front-ends call them. The first attempt kept those
  helpers in `internal/cli`, which deadlocked into an import cycle the moment the CLI needed to
  launch the TUI -- a useful accident, since it forced the shared layer to be named instead of
  living wherever it happened to be written first.

### Fixed

- **`check` reported success for a run that checked nothing.** The summary counted stale packs
  only, so when every registry was unreachable it found zero stale and printed
  `All packs up to date` -- observed here with all four upstreams failing. It now separates
  unresolved packs from up-to-date ones and says `no pack could be checked: 4 of 4 upstreams were
  unreachable`. Inherited from `core/reporters/tty.py`, which has the same defect.

- **`list-packs` printed the wrong version.** It read `current_version` from this repository's
  config, which is only the fallback, so a pack that had already been synced still displayed a
  stale number here while `check` correctly reported it up to date. It now prints the resolved
  version, the same one every other command compares against.

- **`internal/core/pipeline`, `internal/core/checker` and `internal/core/httpx`.** The engine is
  complete and has no third-party dependencies.

  `pipeline` makes Python's Source/Transform/Sink real types rather than the empty marker
  subclasses they were, so passing a sink where a source belongs no longer compiles. Sink order
  stays load-bearing and is documented at the method: filesystem, then version stamp, then git
  branch, so the version that gets committed is the version that shipped.

  `checker` settles an ordering question the Python had wrong. `core/checker.py` opens by
  declaring it "Mirrors PHP IconPackUpdateChecker exactly. The two implementations MUST agree" --
  and they do not. PHP calls `version_compare`; Python builds a tuple of mixed ints and strings.
  For `3.46.0` against `3.46.0-rc1` PHP reports not-stale and Python reports stale, so a
  scheduled sync would have proposed replacing a stable release with a pre-release of itself;
  Python's version also raises `TypeError` outright on a mixed int/string part. **This port
  follows PHP**, which is the declared reference and the correct one, and pins it by running the
  real `php -r 'version_compare(...)'` over 31 real and adversarial pairs -- they agree on every
  one. `TrimVersion` keeps the character-set trim both implementations share rather than
  correcting it here, because that would leave this port out of step with both.

  `httpx` keeps tenacity's four attempts and exponential bounds, and narrows *which* failures
  retry: Python retried every non-2xx, so a 404 cost four attempts and three backoffs to report
  what the first response already said. Downloads stay idempotent by existence, and a failed one
  now leaves no truncated file for the next run to mistake for a cache hit.

- **A Go port begins, alongside the Python rather than replacing it.** The module lives in `src/`
  per the Go layout in `/opensource/CLAUDE.md`, so its import path is
  `github.com/ichava/maintainer-toolkit/src` and a release will need a second `src/vX.Y.Z` tag
  beside `vX.Y.Z` -- Go resolves a subdirectory module through no other ref. Python is still the
  Docker `ENTRYPOINT` and still what the five packs' `sync-upstream.yml` runs every Monday; that
  does not change until the Go side has done a real sync on a real pack.

  First package is `internal/core/config`, because the config directory is a bind-mounted volume
  in production and therefore an external interface: it has to read the four shipped
  `config/*.json` files unchanged, comment keys and all.

  It is verified against Python rather than against fixtures. A differential run over the four
  real configs and the real pack repos produces **identical** output for every field, including
  the V49 convergence that matters most -- `icon-sets-tabler` resolves `3.47.0` from the pack
  repo while this repository's config still says `3.46.0`, and both implementations agree that
  the pack wins.

  Three details are pinned by tests because each is a way the port could have gone quietly wrong:

  - **The writer never creates a key.** `icon-sets-bundled` ships no `package.upstream_version`,
    and `package.version` is the pack's own release number, not the upstream one. Only an
    existing leaf that already differs is rewritten.
  - **The writer preserves the rest of the document byte for byte.** Go map iteration has no
    order, so the obvious unmarshal-modify-marshal would reorder every key and bury a one-line
    version bump in a whole-file diff on the sync PR. The setter splices the value's byte range
    instead, which is why this package has no JSON dependency.
  - **Unknown keys survive inside `source`.** The checker reads `version_field` out of them, and
    both url-typed packs depend on it. The pack schema itself stays closed, so a typo at the top
    level still fails loudly.

  The module has **no third-party dependencies**.

### Fixed

- **The README's link label named the old central docs repo.** The URL was already correct —
  `docs/upstream-tracking.md`, in this repository — while the text beside it still read
  `ichava/documentation/icon-pack-upstream-tracking.md`. The label now names the page the link
  opens.

  **No link checker sees this class.** The label is a code span, not a target, so the link
  resolves and the text next to it is wrong — `lychee` and every `](...)` sweep pass it. Found
  by grepping for `` `…documentation/….md` `` rather than for links, after the estate-wide link
  scan came back at zero.

## [Unreleased]
## [0.1.3] - 2026-09-22

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

- **Dead links to the deleted `ichava/documentation` repository removed.** That repository no
  longer exists, so every cross-reference to it resolved to a 404. The reporting channels in
  `SECURITY.md` were already stated inline and are unchanged; the Code of Conduct now cites the
  Contributor Covenant directly. Historical mentions in this changelog are left as written.

### Fixed

- **The README's link label named the old central docs repo.** The URL was already correct —
  `docs/upstream-tracking.md`, in this repository — while the text beside it still read
  `ichava/documentation/icon-pack-upstream-tracking.md`. The label now names the page the link
  opens.

  **No link checker sees this class.** The label is a code span, not a target, so the link
  resolves and the text next to it is wrong — `lychee` and every `](...)` sweep pass it. Found
  by grepping for `` `…documentation/….md` `` rather than for links, after the estate-wide link
  scan came back at zero.

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
