"""Transform strategies. Each implements :class:`Transform` from `pipeline`.

Public re-exports::

    from ichava_maintainer_toolkit import transforms
    transforms.Sanitise()
    transforms.SubsetTo(subdir="icons/svg")
    transforms.Categorise(by="cldr")
    transforms.Slugify()
    transforms.Indexer(targets=["codepoints", "names"])
"""

from ichava_maintainer_toolkit.core.transforms.categorise import Categorise
from ichava_maintainer_toolkit.core.transforms.indexer import Indexer
from ichava_maintainer_toolkit.core.transforms.sanitise import Sanitise
from ichava_maintainer_toolkit.core.transforms.slugify import Slugify
from ichava_maintainer_toolkit.core.transforms.subset import SubsetTo

__all__ = ["Categorise", "Indexer", "Sanitise", "Slugify", "SubsetTo"]
