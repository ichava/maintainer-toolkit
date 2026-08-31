"""Sink strategies. Each implements :class:`Sink` from `pipeline`.

Public re-exports::

    from ichava_maintainer_toolkit import sinks
    sinks.Filesystem(root="/work/.../files")
    sinks.GitBranch(repo_root="/work/...", branch="chore/sync-{version}")
    sinks.VersionStamp(pack=cfg)
"""

from ichava_maintainer_toolkit.core.sinks.filesystem import Filesystem
from ichava_maintainer_toolkit.core.sinks.git_branch import GitBranch
from ichava_maintainer_toolkit.core.sinks.version_stamp import VersionStamp

__all__ = ["Filesystem", "GitBranch", "VersionStamp"]
