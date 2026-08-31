"""Pre-baked pipelines per pack.

Each recipe is a function ``build(pack: PackConfig) -> Pipeline`` that
composes the right Source + Transforms + Sinks for that pack. The CLI
dispatches to recipes by ``PackConfig.source.type``.
"""

from ichava_maintainer_toolkit.recipes.emoji_sets import build as build_emoji_sets
from ichava_maintainer_toolkit.recipes.simple_npm import build as build_simple_npm

__all__ = ["build_emoji_sets", "build_simple_npm"]
