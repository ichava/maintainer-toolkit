"""Policy enforcement for SVG documents. Pure: no I/O, no pipeline, no console.

Split from ``sanitise.py`` deliberately. The transform there needs the pipeline,
the progress bar and a filesystem tree; none of that is required to answer the
only question that matters -- given these bytes, what does the policy leave? --
and coupling the two would mean the policy rules could only be tested by running
a pipeline. This module imports lxml and the policy reader, nothing else.
"""

from __future__ import annotations

from lxml import etree

from ichava_maintainer_toolkit.core.transforms.svg_policy import (
    allowed_attribute_prefixes,
    allowed_attributes,
    allowed_tags,
    attribute_name,
    deny_attribute_prefixes,
    forbidden_tags,
    fragment_pattern,
    local_name,
    policy,
    style_value_is_safe,
)


class SvgPolicyViolation(RuntimeError):
    """Raised in strict mode when a document violates the policy."""


def _parser() -> etree.XMLParser:
    """A parser that refuses the structural attacks rather than pattern-matching them.

    ``resolve_entities=False`` and ``no_network=True`` are what close XXE and
    billion-laughs; the previous regex implementation had no defence against
    either, because a regex cannot see an entity expansion at all.
    """
    return etree.XMLParser(
        resolve_entities=False,
        no_network=True,
        huge_tree=False,
        remove_comments=bool(policy().get("stripComments", False)),
    )


def sanitise_bytes(raw: bytes, *, also_strip_class: bool = False) -> tuple[bytes, list[str]]:
    """Filter a document to what the policy permits.

    Returns the serialised result and the names of everything removed, so a
    caller can report *what* was wrong rather than only that something was.

    Raises ``etree.XMLSyntaxError`` for input it cannot parse. That is
    deliberate and differs from the runtime parser in ``ichava/core``, which
    recovers from six common author mistakes: this gate runs before a file is
    committed, where the right answer to malformed input is to say so, not to
    guess at what the author meant.
    """
    tree = etree.fromstring(raw, parser=_parser())
    removed: list[str] = []
    _filter_element(tree, removed, also_strip_class=also_strip_class)

    return etree.tostring(tree, xml_declaration=False, encoding="utf-8"), removed


def _filter_element(el: etree._Element, removed: list[str], *, also_strip_class: bool) -> None:
    for child in list(el):
        if not isinstance(child.tag, str):
            continue

        name = local_name(child.tag)

        if name in forbidden_tags() or name not in allowed_tags():
            removed.append(f"<{name}>")
            el.remove(child)
            continue

        _filter_element(child, removed, also_strip_class=also_strip_class)

    for key in list(el.attrib):
        name = attribute_name(key)

        if not attribute_allowed(name, el.attrib[key], also_strip_class=also_strip_class):
            removed.append(name)
            del el.attrib[key]


def attribute_allowed(name: str, value: str, *, also_strip_class: bool = False) -> bool:
    """Whether an attribute survives, by name and then by value.

    Order matters. The deny prefix wins over everything, then the ARIA-style
    allow prefixes, then the name list, and only then the value checks. Being on
    the name list never means the value is trusted -- that distinction is the
    whole design (`block by value, not by name`), and collapsing it is how a
    sanitiser ends up permitting ``href="javascript:…"`` because ``href`` was
    "allowed".
    """
    lowered = name.lower()

    if any(lowered.startswith(prefix) for prefix in deny_attribute_prefixes()):
        return False

    if also_strip_class and lowered == "class":
        return False

    if any(lowered.startswith(prefix) for prefix in allowed_attribute_prefixes()):
        return True

    if lowered not in {a.lower() for a in allowed_attributes()}:
        return False

    if lowered in {"href", "xlink:href"}:
        return bool(fragment_pattern().match(value))

    if lowered == "style":
        return style_value_is_safe(value)

    return True
