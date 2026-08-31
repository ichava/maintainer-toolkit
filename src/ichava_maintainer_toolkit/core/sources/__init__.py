"""Source strategies. Each one implements :class:`Source` from `pipeline`.

Public re-exports::

    from ichava_maintainer_toolkit import sources
    sources.NpmTarball("@x/y", version="1.0.0")
    sources.GithubArchive(owner="x", repo="y", version="v1.0.0")
    sources.UnicodeCldr(unicode_version="17.0")
"""

from ichava_maintainer_toolkit.core.sources.github_archive import GithubArchive
from ichava_maintainer_toolkit.core.sources.npm_tarball import NpmTarball
from ichava_maintainer_toolkit.core.sources.unicode_cldr import UnicodeCldr

__all__ = ["GithubArchive", "NpmTarball", "UnicodeCldr"]
