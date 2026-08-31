[← Package README](../README.md#documentation)

# Getting started

*Tutorial.*

The toolkit answers one question: *has upstream moved, and can we take it?*, and then does the
taking.

## See what it knows about

```bash
ichava-maintainer-toolkit list-packs
```

Every pack in `config/packs.json`, with the version each currently vendors.

## Check without writing anything

```bash
ichava-maintainer-toolkit check
```

Resolves each pack's `version_check_url`, compares against what the pack repo records, and reports
what is stale. Writes nothing. This is the command to run when you want an answer rather than a
pull request.

## Refresh one pack

```bash
ichava-maintainer-toolkit sync --pack=tabler-icons
```

Fetches the new upstream, runs the pack's recipe, writes the assets, records the new version in the
pack's own `config.json`, commits the lot to a branch and opens a pull request. A human reviews the
diff and merges, the toolkit never merges.

Add `--dry-run` to do everything except the commit.

## Try it interactively

```bash
ichava-maintainer-toolkit menu
```

The default Docker command. Same operations, with the pack list and the confirmations in front of
you.

## What happens next

The pull request lands in the pack repository. **It arrives with no CI checks**: a pull request
opened with `GITHUB_TOKEN` does not trigger workflows, by design, so verify locally before merging.
Merging to `main` runs the pack's suite normally.

---

[← Docs index](../README.md#documentation)
