# Changelog

All notable changes to `ichava/maintainer-toolkit` follow [Keep a Changelog](https://keepachangelog.com/en/1.0.0/) and [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

- **Rename rules, so a flat set vendored from a multi-variant upstream can be refreshed.** Three
  sets commit several upstream directories as one flat directory, distinguishing the variants by
  an affix on the filename, and a manifest `path` cannot express that. The failure would not have
  been a partial refresh but a destructive one: the sink wipes the destination first, so pointing
  `path` at heroicons' `24/solid` refreshes 324 icons and **deletes the other 964**.

  A set may now declare `variants` instead, each with its own `path` and an optional `prefix` or
  `suffix`, plus an optional `separator` when the pack and its upstream spell word breaks
  differently. The rules were recovered by measurement rather than guessed, and the shipped Go
  transform was run over the real packages to confirm it:

  | Set | Upstream | Produced | Committed | Byte-identical | New | Dropped |
  |---|---|---:|---:|---:|---:|---:|
  | `heroicons` | `24/outline` `24/solid` `20/solid` `16/solid` as `o- s- m- c-` | 1,288 | 1,288 | **1,288** | 0 | 0 |
  | `teeny-icons` | `solid` bare + `outline` as `-o` | 1,200 | 1,200 | 0 | 0 | 0 |
  | `google-material-design-icons` | five variants as bare `-o -r -s -tt` | 10,610 | 10,751 | 0 | 0 | 141 |

  **heroicons reproduces every committed icon byte for byte**, which is as strong as evidence for
  a recovered rule gets. `teeny-icons` matches every name but no bytes -- the pack reformatted
  them onto `fill="currentColor"` -- and gmdi drops 141 icons upstream has retired, at 98%
  retention. **None of the three produces an icon the pack does not already have**, which is what
  rules out an affix rule that happens to cover the set while meaning something else.

  `google-material-design-icons` also needs `"separator": "-"`: upstream ships `18_up_rating`
  against the pack's `18-up-rating`, and without the rewrite only 607 of 2,168 names per variant
  line up -- which is exactly why the audit had been reporting it as a partial match against the
  right package.

  Two ways this loses an icon quietly are refused rather than tolerated. **A collision** -- an
  upstream icon whose own name ends in another variant's suffix, so `outline/foo.svg` and
  `solid/foo-o.svg` both flatten onto `foo-o.svg` -- fails the run naming both sources, because
  the second copy would replace the first at an unchanged file count and the retention guard
  would see a clean swap. There are none in this pack today, which is a fact about today's
  upstreams and not a property of the scheme. **A variant that resolves to a missing or empty
  directory** likewise fails, rather than refreshing a fraction of the set over a wipe.

  The manifest now requires exactly one of `path` and `variants`: both is ambiguous about which
  the refresh honours, and neither leaves the entry pointed at the package root, which for most
  upstreams is a README and a licence rather than icons.

- **The audit reports a tie it cannot break instead of returning one.** A package that ships
  sibling variant directories -- `cryptocurrency-icons` has `svg/{black,white,color,icon}`, each
  with the same 483 filenames -- defeats name scoring completely: all four cover the set equally,
  and when the pack vendored an older release none of them is byte-identical either. The scorer
  was then returning whichever directory `WalkDir` reached last and grading it `package`.

  It reached `svg/white`. The committed icons are `svg/black` with `fill="currentColor"` added to
  the root, so a refresh would have replaced every themeable icon with a hard-coded `#FFF` one,
  invisible on a light background -- **at an identical file count, so the retention guard would
  have waved it straight through.** Re-auditing the 28 entries the manifest had accepted found
  five such ties, and three of the five had resolved to the wrong directory.

  Ties are now settled where they can be measured and reported where they cannot:

  - A rival holding the *same bytes* under a second path is a packaging duplicate, not a choice,
    and is excluded. `ionicons` ships its icons at `dist/ionicons/svg` and again under
    `dist/collection/components/icon/svg` -- byte-identical to each other.
  - A rival that genuinely differs is compared by resemblance to the committed icons, and the
    tie is settled when one is clearly closest. That fixes `fontawesome` (`svgs`, not FA7's
    640x640 `svgs-full` against a committed 448x512) and `pepicons` (`svg/pop`, not `svg/print`).
  - Anything closer than that grades **`ambiguous`**: the right package, an undecidable variant.
    It is a separate grade from `partial` because the remaining work differs in kind -- a partial
    result needs a better candidate package, an ambiguous one needs a human to name the variant.

  The resemblance metric has a stated limit rather than a tuned threshold. Trigrams over a whole
  file are dominated by the path data, so two variants drawing the identical shape and differing
  only in a colour attribute score nearly the same -- exactly `cryptocurrency-icons`. Those stay
  ambiguous. Tuning the margin until it happened to pick `black` would fit the constant to two
  cases and settle the next one wrongly and silently.

- **The manifest's acceptance floor is the refresh's retention floor**, one constant rather than
  two literals. A set was admitted at 95% name coverage while the refresh refuses to replace a
  directory below 90% retention, so the audit rejected entries a refresh would have accepted and
  admitted none it would refuse. Measured, 95% excluded `@mapbox/maki` for `maki-icons` at 198 of
  211 -- unmistakably the right project, rejected over thirteen retired icons. `recipes` now reads
  `bundled.PackageCoverageFloor` directly, and a test pins the two together: two independent
  literals would drift, and the drift would surface as a Monday cron refusing sets the audit
  approved on Friday, with an error about upstream reorganising that would be the wrong
  explanation entirely.

- **`config/icon-sets-bundled.sets.json` carries 35 sets** covering 59,769 of the pack's 121,314
  icons -- 49%, against the 21 sets and 38% the first audit established. Two are settled by inspection rather than by
  the scorer and say so in their notes, with the evidence: `cryptocurrency-icons` to `svg/black`
  and `ionicons` to `dist/svg`, whose sibling expresses the same drawings as
  `class="ionicon-fill-none ionicon-stroke-width"` and renders as nothing without ionicons' own
  stylesheet. `teeny-icons` was withdrawn -- its 1,200 committed icons merge upstream's `solid/`
  and `outline/` with an `-o` suffix, which no single path can express; it now grades `partial`
  at 600 of 1,200 and needs a rename rule, as `heroicons` and `google-material-design-icons` do.

- **The per-set native refresh for `icon-sets-bundled`.** One pipeline per set, each from that
  set's own upstream, so every set keeps the SVG dialect it actually ships in rather than being
  rewritten into a uniform one. Driven by `config/icon-sets-bundled.sets.json`, seeded from the
  audit with the 21 sets it established.

  The manifest is a separate file rather than a key in the pack config, deliberately and
  temporarily: the Python is still the Docker entrypoint, its `PackConfig` rejects unknown keys,
  and a new key would break every Python command for this pack during the overlap. Verified --
  the Python still loads all four configs with the new file present. Fold it in once the Python
  is gone.

  Three properties, each of which exists because of something measured while building it:

  - **A set with no manifest entry is not touched at all.** Not fetched, not wiped. The audit
    resolves 21 of 72, and the remaining 51 must stay exactly as they are; a manifest is the
    input to something that wipes a directory, so absent is safe and present-but-wrong replaces a
    set's icons with another project's.
  - **The refresh refuses to shrink a set by more than 10%.** Not hypothetical: iconoir's
    upstream reorganised into per-weight directories, so the audited path now covers 1,383 of the
    committed 1,671 icons. Without the guard a scheduled run would have deleted 288 and reported
    success. It counts before it removes, so a refusal leaves the pack untouched -- confirmed
    against the real set, which still had all 1,671 afterwards.
  - **The policy is reported, not enforced.** This pack has never been sanitised: its icons keep
    comments the policy strips, and every fontawesome file sampled changes under it -- including
    the embedded Font Awesome licence notice. Enforcing during a refresh would rewrite tens of
    thousands of files and drop that attribution, so violations are counted and surfaced and the
    bytes are left as upstream shipped them. Enabling it is a decision for the pack's owner, not
    a side effect of a Monday cron.

  Verified end to end against the live registries: all 21 sets fetch, subset and copy, and
  `bootstrap-icons` reproduces its 2,078 committed icons **byte for byte, zero differing**.
  `feather-icons` differs by exactly one file, an icon upstream has added since.

### Fixed

- **The audit recorded paths in a frame the recipe could not read.** It reported them relative to
  the extraction root (`package/icons`) while `NpmTarball` hands the *inside* of the npm wrapper
  downstream, so every `SubsetTo` failed with "subdir not found". The audit now resolves inside
  `package/`, which is the frame the pipeline uses.

- **A set tracking `latest` stamped the literal string "latest" as its version.** `NpmTarball`
  now records what npm resolved -- read from the tarball filename -- so the version in the logs,
  the metrics and any stamp is a version rather than a word that compares as one.

- **The HTTP transport keeps its proxy, and `SSL_CERT_FILE` is loaded explicitly.**

  The transport is *cloned* from `http.DefaultTransport` rather than built fresh, and that is the
  load-bearing half. A hand-rolled `&http.Transport{TLSClientConfig: …}` silently drops
  `Proxy: http.ProxyFromEnvironment`: every request then dials directly and fails as
  `no such host`, which reads like DNS rather than like the configuration mistake it is. Pinned
  by a test that asserts a bare `&http.Transport{}` has no Proxy and a clone does. Two sessions
  confirmed this independently.

  The certificate half is defensive rather than established, and the code says so. Go is
  documented to honour `SSL_CERT_FILE`, and on Linux -- production, since the image is Linux --
  it does. On the macOS machine this was developed on it demonstrably did not: with the variable
  visible to the process, `x509.SystemCertPool()` returned zero subjects and every TLS dial
  failed, while `gh` on the same toolchain in the same shell succeeded. A second session measured
  the opposite and neither could reproduce the other, so the bundle is loaded explicitly, which
  is correct under either reading -- a no-op where Go already honours the variable, and a fix
  where it does not.

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
