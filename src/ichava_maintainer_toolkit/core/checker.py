"""Upstream-version checker. Mirrors PHP IconPackUpdateChecker exactly.

The two implementations MUST agree: PHP powers host-app discovery,
this Python copy drives the maintainer refresh. Drift breaks the
contract that makes the maintainer-side `make check` consistent with
`php artisan ichava:icons:check-updates`.
"""

from __future__ import annotations

import logging
import re
from dataclasses import dataclass
from typing import Any

from ichava_maintainer_toolkit.core.config import (
    PackConfig,
    SourceConfig,
    resolved_current_version,
)
from ichava_maintainer_toolkit.core.http import get_json

logger = logging.getLogger(__name__)


@dataclass(frozen=True)
class CheckResult:
    package: str
    current: str | None
    latest: str | None
    stale: bool
    release_url: str | None
    reason: str | None = None


def check_pack(pack: PackConfig) -> CheckResult:
    """Resolve the latest version + compare against what the pack repo vendors.

    The comparison base is `resolved_current_version`, which reads the pack
    repo's own config first. Comparing against this repository's
    `current_version` is what made the sync non-convergent (V49): the bump was
    written into the runner's throwaway checkout and never committed, so every
    run saw the same stale value.
    """
    current = resolved_current_version(pack)

    if not pack.version_check_url:
        return CheckResult(pack.pack, current, None, False, None, "no version_check_url")
    try:
        payload = get_json(pack.version_check_url)
    except Exception as e:
        return CheckResult(pack.pack, current, None, False, None, f"unreachable: {e}")

    latest = parse_latest(pack.source, payload)
    if latest is None:
        return CheckResult(pack.pack, current, None, False, None, "could not parse latest")

    return CheckResult(
        package=pack.pack,
        current=current,
        latest=latest,
        stale=is_stale(current, latest),
        release_url=resolve_release_url(pack.source, payload, latest),
    )


def parse_latest(source: SourceConfig, payload: Any) -> str | None:
    t = source.type
    if t == "npm" and isinstance(payload, dict):
        return _trim_v(payload.get("version"))
    if t == "github-tag" and isinstance(payload, list) and payload:
        first = payload[0] if isinstance(payload[0], dict) else {}
        return _trim_v(first.get("name") or first.get("ref"))
    if t == "github-release" and isinstance(payload, dict):
        return _trim_v(payload.get("tag_name"))
    if t == "url" and isinstance(payload, dict):
        # Walk a dot-path, default to "version" if not declared.
        path = (source.model_extra or {}).get("version_field", "version")
        cursor: Any = payload
        for seg in path.split("."):
            if not isinstance(cursor, dict) or seg not in cursor:
                return None
            cursor = cursor[seg]
        return _trim_v(cursor) if isinstance(cursor, str) else None
    return None


def resolve_release_url(source: SourceConfig, payload: Any, version: str | None) -> str | None:
    """Synthesise a clickable release URL per source type."""
    t = source.type
    if t == "github-release" and isinstance(payload, dict) and payload.get("html_url"):
        return payload["html_url"]
    if t in ("github-release", "github-tag") and source.owner and source.repo:
        return (
            f"https://github.com/{source.owner}/{source.repo}/releases/tag/v{version}"
            if version
            else f"https://github.com/{source.owner}/{source.repo}"
        )
    if t == "npm" and source.package:
        return (
            f"https://www.npmjs.com/package/{source.package}/v/{version}"
            if version
            else f"https://www.npmjs.com/package/{source.package}"
        )
    return None


def is_stale(current: str | None, latest: str) -> bool:
    if not current:
        return True
    return _semver_lt(current, latest)


def _trim_v(tag: str | None) -> str | None:
    if not tag:
        return None
    return tag.lstrip("vV ").strip() or None


def _semver_lt(a: str, b: str) -> bool:
    """Naive semver compare: tuple-of-(int|str). Falls back to lexical safely."""
    return _parse(a) < _parse(b)


def _parse(v: str) -> tuple:
    return tuple(int(c) if c.isdigit() else c for c in re.split(r"[.\-+]", v))
