"""Checker parity tests -- mirror PHP IconPackUpdateCheckerTest cases."""

from __future__ import annotations

from ichava_maintainer_toolkit.core.checker import (
    is_stale,
    parse_latest,
    resolve_release_url,
)
from ichava_maintainer_toolkit.core.config import SourceConfig


def src(**kw):
    return SourceConfig(**kw)


def test_parse_latest_npm():
    assert parse_latest(src(type="npm", package="@x/y"), {"version": "5.4.2"}) == "5.4.2"


def test_parse_latest_github_release():
    assert (
        parse_latest(
            src(type="github-release", owner="x", repo="y"),
            {"tag_name": "v17.1.0", "html_url": "https://example.com/v17.1.0"},
        )
        == "17.1.0"
    )


def test_parse_latest_github_tag_first_entry():
    assert (
        parse_latest(
            src(type="github-tag", owner="x", repo="y"),
            [{"name": "v7.4.0"}, {"name": "v7.3.9"}],
        )
        == "7.4.0"
    )


def test_parse_latest_url_with_dot_path():
    s = src(type="url", version_field="dist-tags.latest")
    assert parse_latest(s, {"dist-tags": {"latest": "9.9.9"}}) == "9.9.9"


def test_resolve_release_url_npm_synthesises():
    s = src(type="npm", package="@twemoji/svg")
    assert (
        resolve_release_url(s, {}, "17.1.0")
        == "https://www.npmjs.com/package/@twemoji/svg/v/17.1.0"
    )


def test_resolve_release_url_github_tag_synthesises():
    s = src(type="github-tag", owner="lipis", repo="flag-icons")
    assert (
        resolve_release_url(s, {}, "7.4.0")
        == "https://github.com/lipis/flag-icons/releases/tag/v7.4.0"
    )


def test_resolve_release_url_github_release_uses_html_url():
    s = src(type="github-release", owner="x", repo="y")
    payload = {"tag_name": "v1.0.0", "html_url": "https://example.com/v1.0.0"}
    assert resolve_release_url(s, payload, "1.0.0") == "https://example.com/v1.0.0"


def test_is_stale_when_current_lower():
    assert is_stale("3.0.0", "3.44.0") is True


def test_is_stale_when_current_equal():
    assert is_stale("3.44.0", "3.44.0") is False


def test_is_stale_when_no_current():
    assert is_stale(None, "3.0.0") is True
