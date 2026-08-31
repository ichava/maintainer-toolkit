[← Package README](../README.md#documentation)

# Configuration

*Reference.*

One JSON file per pack in `config/`, plus `config/packs.json` as the registry. The schema is
enforced at load time by a Pydantic model, so a malformed config fails before anything reaches an
upstream registry.

## The registry

```json
{ "packs": ["tabler-icons", "flag-icons", "bundled-icons", "emoji-sets"] }
```

Each slug maps to `config/<slug>.json`.

## A pack config

| Key | Meaning |
|---|---|
| `name` | pack slug; must match the filename |
| `pack` | Composer package name, e.g. `ichava/tabler-icons` |
| `pack_root` | path to the pack repo, absolute or relative to the config dir |
| `version_check_url` | the URL polled to discover the latest upstream version |
| `source` | how to fetch: `npm`, `github-archive`, `github-tag`, `github-release`, `url`, `script` |
| `sinks` | where refreshed assets land: `filesystem`, then `git-branch` |
| `transforms` | per-transform config; each entry needs a `type` |
| `version_file` | pack-repo-relative file recording the vendored version. Defaults to `resources/assets/svg/config.json` |
| `version_keys` | dot-paths inside that file holding the version |
| `current_version` | **fallback only**: see below |

Keys beginning with `_` are authoring comments and are dropped before validation.

## Where the vendored version actually lives

**In the pack repo, not here.** `version_file` and `version_keys` point at the pack's own
`resources/assets/svg/config.json`, which travels with the assets it describes.

`current_version` in this repository is a fallback for a pack that has no vendored record yet. It
used to be the source of truth, and that made the sync non-convergent: the bump was written through
the `/app/config` mount, the runner's throwaway checkout, and never committed, so every scheduled
run re-detected the same release and re-opened the same pull request. The `VersionStamp` sink now
writes into the pack repo, before `GitBranch` commits.

## Adding a pack

1. Write `config/<slug>.json`.
2. Add the slug to `config/packs.json`.
3. Confirm it loads: `ichava-maintainer-toolkit check --pack=<slug>`.

If no existing recipe fits the upstream's shape, see [architecture](architecture.md) for where a new
one goes.

---

[← Docs index](../README.md#documentation)
