"""The shared SVG policy, and the build-time gate that enforces it.

This is one of four runtimes reading `svg-policy.json`; the others are
`ichava/core` (PHP) and the two TypeScript clients. Their equivalents are
`core/tests/Unit/SvgPolicyTest.php`,
`react-browser/src/core/svgPolicy.test.ts` and
`browser/resources/assets/scripts/ichava-ts/security/svgPolicy.test.ts`, and the
fixtures below are deliberately the same. Not identical output -- the
serialisers differ -- but the same constructs surviving, which is R2.
"""

from __future__ import annotations

import hashlib
from pathlib import Path

import pytest
from lxml import etree

from ichava_maintainer_toolkit.core.transforms.svg_filter import (
    attribute_allowed,
    sanitise_bytes,
)
from ichava_maintainer_toolkit.core.transforms.svg_policy import (
    POLICY_PATH,
    allowed_attributes,
    policy,
)

# Pinned digest of the vendored copy of core/resources/security/svg-policy.json.
# This package has no dependency on ichava/core and cannot reach it, so the
# digest catches a local edit; catching that core has moved on is the job of
# maintainer-toolkit/.scripts/sync-svg-policy.mjs, the one place every checkout
# is visible at once. On a policy change: sync with --write, paste what it prints.
PINNED_SHA256 = "8794a59bdf3fe3112eccc68c157d85c1c55728299dedcdbd7447ccbc083a6be2"

SVG = 'xmlns="http://www.w3.org/2000/svg"'


class TestVendoredPolicy:
    def test_has_not_drifted_from_the_digest_it_was_synced_at(self) -> None:
        digest = hashlib.sha256(Path(POLICY_PATH).read_bytes()).hexdigest()

        assert digest == PINNED_SHA256

    def test_merges_the_value_restricted_names_into_the_by_name_allow_list(self) -> None:
        # style and href live in their own policy blocks because their VALUES are
        # checked. Reading only allowedAttributes strips style, the sole paint
        # source for 261 of 501 metronic icons. Same trap as the PHP and TS readers.
        assert "style" in allowed_attributes()
        assert "href" in allowed_attributes()
        assert "style" not in policy()["allowedAttributes"]


class TestTheRegexCouldNotSeeThis:
    """The cases that motivated moving off regex.

    `_EVENT_ATTR` was `\\s+on[a-z]+\\s*=\\s*"[^"]*"` -- double quotes only. Every
    other spelling passed the build-time gate untouched, which made the runtime
    that ran first the weakest of the four.
    """

    @pytest.mark.parametrize(
        "raw",
        [
            f'<svg {SVG}><path onload="x()" d="M0 0"/></svg>'.encode(),
            b"<svg xmlns='http://www.w3.org/2000/svg'><path onload='x()' d='M0 0'/></svg>",
            f'<svg {SVG}><path ONLOAD="x()" d="M0 0"/></svg>'.encode(),
        ],
        ids=["double-quoted", "single-quoted", "uppercase"],
    )
    def test_strips_an_event_handler_however_it_is_spelled(self, raw: bytes) -> None:
        out, removed = sanitise_bytes(raw)

        assert b"onload" not in out.lower()
        assert b'd="M0 0"' in out
        assert removed

    def test_strips_a_foreign_object(self) -> None:
        out, _ = sanitise_bytes(
            f'<svg {SVG}><foreignObject><b>x</b></foreignObject><path d="M0 0"/></svg>'.encode()
        )

        assert b"foreignObject" not in out
        assert b"<path" in out

    def test_refuses_rather_than_guesses_at_unparsable_input(self) -> None:
        # An unquoted attribute is not silently passed through. The runtime
        # parser in ichava/core deliberately recovers from author mistakes; this
        # gate runs before a file is committed, where saying so is the right answer.
        with pytest.raises(etree.XMLSyntaxError):
            sanitise_bytes(f"<svg {SVG}><path onload=x() d='M0 0'/></svg>".encode())


class TestParityWithTheOtherRuntimes:
    def test_keeps_the_style_attribute(self) -> None:
        out, _ = sanitise_bytes(f'<svg {SVG}><path style="fill:#123456" d="M0 0"/></svg>'.encode())

        assert b"fill:#123456" in out

    def test_rejects_a_style_value_that_reaches_off_the_document(self) -> None:
        out, _ = sanitise_bytes(
            f'<svg {SVG}><path style="fill:url(https://evil.test/x)" d="M0 0"/></svg>'.encode()
        )

        assert b"evil.test" not in out

    def test_keeps_a_fragment_reference_and_drops_an_external_one(self) -> None:
        kept, _ = sanitise_bytes(f'<svg {SVG}><use href="#ok"/></svg>'.encode())
        dropped, _ = sanitise_bytes(f'<svg {SVG}><use href="https://evil.test/x"/></svg>'.encode())

        assert b"#ok" in kept
        assert b"evil.test" not in dropped

    def test_keeps_gradient_geometry_and_stops(self) -> None:
        out, _ = sanitise_bytes(
            f'<svg {SVG}><defs><linearGradient id="g" x1="0" y1="0" x2="1" y2="1">'
            f'<stop offset="0" stop-color="#f00"/></linearGradient></defs></svg>'.encode()
        )

        assert b'x1="0"' in out
        assert b"stop-color" in out

    def test_keeps_filters_and_patterns(self) -> None:
        out, _ = sanitise_bytes(
            f'<svg {SVG}><defs><filter id="f"><feGaussianBlur stdDeviation="2"/></filter>'
            f'<pattern id="p" patternUnits="userSpaceOnUse"><path d="M0 0h4"/></pattern>'
            f"</defs></svg>".encode()
        )

        assert b"feGaussianBlur" in out
        assert b"patternUnits" in out

    def test_keeps_the_accessible_name_and_its_wiring(self) -> None:
        out, _ = sanitise_bytes(
            f'<svg {SVG} role="img" aria-labelledby="t"><title id="t">Home</title>'
            f'<path d="M0 0"/></svg>'.encode()
        )

        assert b"<title" in out
        assert b"aria-labelledby" in out

    def test_still_blocks_the_style_element(self) -> None:
        out, _ = sanitise_bytes(
            f'<svg {SVG}><style>.a{{fill:red}}</style><path style="fill:#123456" d="M0 0"/></svg>'.encode()
        )

        assert b"<style" not in out
        assert b"fill:#123456" in out


class TestAttributeOrdering:
    """Being on the name list never means the value is trusted."""

    def test_deny_prefix_wins_over_everything(self) -> None:
        assert attribute_allowed("onclick", "x()") is False

    def test_aria_is_allowed_by_shape(self) -> None:
        assert attribute_allowed("aria-labelledby", "t") is True

    def test_an_allowed_name_can_still_fail_on_its_value(self) -> None:
        assert attribute_allowed("href", "#ok") is True
        assert attribute_allowed("href", "javascript:alert(1)") is False
        assert attribute_allowed("style", "fill:#fff") is True
        assert attribute_allowed("style", "background:url(https://evil.test/x)") is False

    def test_also_strip_class_is_cosmetic_not_security(self) -> None:
        assert attribute_allowed("class", "icon", also_strip_class=False) is True
        assert attribute_allowed("class", "icon", also_strip_class=True) is False
