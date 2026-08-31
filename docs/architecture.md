[← Package README](../README.md#documentation)

# Architecture

*Explanation.*

## A pipeline of stages

Every refresh is the same shape: fetch, narrow, clean, write, commit.

```
Source  ->  Transforms       ->  Sinks
npm         SubsetTo             Filesystem
github      Sanitise             VersionStamp
url         Categorise           GitBranch
script      Slugify / Indexer
```

`core/pipeline.py` runs the stages in order and carries a `StageContext` between them. A stage that
raises stops that pack and leaves the others alone.

## Why the parts are separate

**Sources** differ by upstream, not by pack: three packs pull npm tarballs, and the code for that
should exist once. **Transforms** are per-pack policy, which subdirectory to take, how to derive a
category. **Sinks** are where the result goes, and the ordering among them is load-bearing:
`Filesystem` writes the assets, `VersionStamp` records the version, and `GitBranch` commits
everything uncommitted in the pack repo. Stamping after the commit would record a version that
never shipped.

## Why the version lives in the pack repo

`GitBranch` commits the pack repo, so anything written before it travels in the pull request.
Anything written to *this* repository does not: under Docker, `config/` is a mount from the runner's
checkout, deleted by the workflow's cleanup step.

That is not hypothetical. `current_version` used to live only here, the bump was written through
that mount, and it was discarded on every run, so the checker compared against a version that never
advanced and re-opened the same pull request indefinitely. Moving the record into the pack, beside
the assets it describes, is what makes the sync converge.

## Why a human merges

The toolkit opens a pull request and stops. An upstream refresh can change thousands of files, and
"upstream released a version" is not evidence that the result renders. The review is the point of
the workflow, not a formality around it.

## Recipes

A recipe assembles the pipeline for one pack. `simple_npm` covers the common case: npm tarball,
subset, sanitise, write, stamp, commit. `emoji_sets` is the exception: three upstreams feeding three
filesystem sinks, then one trailing pipeline that only commits.

Add a recipe when an upstream's shape genuinely differs. Adding one because a pack wants a different
subdirectory means the config is missing a knob.

---

[← Docs index](../README.md#documentation)
