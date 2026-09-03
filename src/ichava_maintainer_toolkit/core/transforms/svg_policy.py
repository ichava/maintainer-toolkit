"""Reader for the shared SVG policy.

``svg-policy.json`` beside this module is a byte-identical copy of
``core/resources/security/svg-policy.json``. It is vendored because this package
has no Composer dependency on ``ichava/core`` and cannot reach it; the copy is
kept honest by ``maintainer-toolkit/.scripts/sync-svg-policy.mjs`` and pinned by
digest in ``tests/unit/test_svg_policy.py``.

Do not hand-edit the JSON. Edit the canonical file, run the sync script with
``--write``, update the pinned digest.
"""

from __future__ import annotations

import json
import re
from functools import lru_cache
from pathlib import Path

POLICY_PATH = Path(__file__).with_name("svg-policy.json")

SVG_NS = "http://www.w3.org/2000/svg"
XLINK_NS = "http://www.w3.org/1999/xlink"


@lru_cache(maxsize=1)
def policy() -> dict:
    """The parsed policy.

    Deliberately not wrapped in a try/except. A policy that silently falls back
    to an empty dict strips every element from every icon, and one that falls
    back to a permissive default is a security hole; both look like a working
    install until something renders.
    """
    return json.loads(POLICY_PATH.read_text(encoding="utf-8"))


@lru_cache(maxsize=1)
def allowed_tags() -> frozenset[str]:
    return frozenset(policy()["allowedTags"])


@lru_cache(maxsize=1)
def forbidden_tags() -> frozenset[str]:
    return frozenset(policy()["forbiddenTags"])


@lru_cache(maxsize=1)
def allowed_attributes() -> frozenset[str]:
    """The by-name allow-list, which is NOT simply ``allowedAttributes``.

    The policy keeps value-restricted attributes in their own blocks -- ``style``
    under ``styleAttribute``, ``href``/``xlink:href`` under ``fragmentOnlyRefs``
    -- so a reader that takes only ``allowedAttributes`` strips ``style``, the
    sole paint source for 261 of 501 metronic icons. The PHP and both TypeScript
    readers make the same merge; keeping them identical is the point of having
    one policy.

    On this list means the NAME may appear. The value is still checked.
    """
    names = set(policy()["allowedAttributes"])

    if policy().get("styleAttribute"):
        names.add("style")

    names.update(policy().get("fragmentOnlyRefs", {}).get("attributes", []))

    return frozenset(names)


@lru_cache(maxsize=1)
def allowed_attribute_prefixes() -> tuple[str, ...]:
    return tuple(policy().get("allowedAttributePrefixes", []))


@lru_cache(maxsize=1)
def deny_attribute_prefixes() -> tuple[str, ...]:
    return tuple(policy().get("denyAttributePrefixes", []))


@lru_cache(maxsize=1)
def fragment_pattern() -> re.Pattern[str]:
    return re.compile(policy()["fragmentOnlyRefs"]["allow"])


_STYLE_SINKS = ("expression(", "behavior:", "-moz-binding", "@import")
_URL_CALL = re.compile(r"""url\(\s*(['"]?)([^'")]*)\1\s*\)""", re.IGNORECASE)


def style_value_is_safe(value: str) -> bool:
    """A ``url()`` aimed off the document is the CSS exfiltration vector."""
    lowered = value.lower()

    if any(sink in lowered for sink in _STYLE_SINKS):
        return False

    return all(match.group(2).startswith("#") for match in _URL_CALL.finditer(value))


def local_name(tag: object) -> str:
    """Strip the namespace lxml prefixes onto every tag and attribute name."""
    if not isinstance(tag, str):
        return ""

    return tag.rsplit("}", 1)[-1] if tag.startswith("{") else tag


def attribute_name(name: str) -> str:
    """Normalise an lxml attribute key back to the policy's spelling.

    ``xlink:href`` arrives as ``{http://www.w3.org/1999/xlink}href``, and the
    policy names it ``xlink:href``. Everything else loses its namespace.
    """
    if name.startswith(f"{{{XLINK_NS}}}"):
        return "xlink:" + name.split("}", 1)[1]

    return local_name(name)
