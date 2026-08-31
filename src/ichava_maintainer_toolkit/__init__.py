"""ichava-maintainer-toolkit: maintainer toolkit for the Ichava icon-pack ecosystem.

Public API surface -- import these directly when wiring a custom recipe:

    from ichava_maintainer_toolkit import Pipeline, sources, transforms, sinks

    Pipeline.named("my-pack")
        .source(sources.NpmTarball("@x/y", version="1.0.0"))
        .transform(transforms.Sanitise())
        .sink(sinks.Filesystem(root="/work/x/resources/assets/svg/files"))
        .run()

Everything else lives under `ichava_maintainer_toolkit.core.*` and `ichava_maintainer_toolkit.recipes.*`.
"""

from ichava_maintainer_toolkit.core import sinks, sources, transforms
from ichava_maintainer_toolkit.core.pipeline import Pipeline, Stage, StageContext
from ichava_maintainer_toolkit.version import __version__

__all__ = [
    "Pipeline",
    "Stage",
    "StageContext",
    "__version__",
    "sinks",
    "sources",
    "transforms",
]
