"""Recipe: NPM tarball -> SubsetTo -> Sanitise -> Filesystem -> GitBranch.

Covers the common case: a pack that mirrors one upstream npm package
where the SVGs live under ``{source_path}/`` inside the tarball.
Used by tabler-icons (`@tabler/icons` -> `icons/{outline,filled}/`)
and flag-icons (`flag-icons` -> `flags/{4x3,1x1}/`).

For multi-source packs (emoji-sets pulls Twemoji + OpenMoji + CLDR)
write a dedicated recipe.
"""

from __future__ import annotations

from ichava_maintainer_toolkit.core.config import PackConfig, interpolate
from ichava_maintainer_toolkit.core.pipeline import Pipeline
from ichava_maintainer_toolkit.core.sinks import Filesystem, GitBranch, VersionStamp
from ichava_maintainer_toolkit.core.sources import NpmTarball
from ichava_maintainer_toolkit.core.transforms import Sanitise, SubsetTo


def build(pack: PackConfig, *, version: str, dry_run: bool = False) -> Pipeline:
    if pack.source.type != "npm":
        raise ValueError(f"simple_npm recipe doesn't apply to source.type={pack.source.type}")
    if not pack.source.package:
        raise ValueError("simple_npm: source.package is required")

    pipe = (
        Pipeline.named(f"{pack.name}@{version}")
        .config(
            pack=pack.pack, pack_root=pack.pack_root, old_version=pack.current_version or "unknown"
        )
        .source(NpmTarball(pack.source.package, version=version))
    )

    if pack.source.source_path:
        pipe.transform(SubsetTo(pack.source.source_path))

    pipe.transform(Sanitise())

    for sink_cfg in pack.sinks:
        if sink_cfg.type == "filesystem":
            root = sink_cfg.root or ""
            root = interpolate(root, pack_root=pack.pack_root, version=version)
            pipe.sink(Filesystem(root=root))
        elif sink_cfg.type == "git-branch":
            if dry_run:
                continue
            # Stamp before committing: GitBranch commits everything uncommitted
            # in the pack repo, so the version record ships in the same commit
            # as the assets it describes (V49).
            pipe.sink(VersionStamp(pack=pack))
            pipe.sink(
                GitBranch(
                    repo_root=sink_cfg.repo_root or pack.pack_root,
                    branch=sink_cfg.branch or "chore/sync-upstream-{version}",
                )
            )

    return pipe
