"""JSON-config loader + Pydantic schema for per-pack configs.

Configs live in ``config/<pack-slug>.json``; the registry sits at
``config/packs.json``. Each pack-config declares:

* upstream coordinates (so we can poll for new versions)
* the recipe to run when refreshing (see `ichava_maintainer_toolkit.recipes`)
* sink targets (where the refreshed assets land)

Schema is enforced at load-time so a malformed config fails loudly
before we start hitting upstream registries.
"""

from __future__ import annotations

import json
import os
from pathlib import Path
from typing import Annotated, Any, Literal

from pydantic import BaseModel, ConfigDict, Field, field_validator, model_validator

# Where configs live by default. Can be overridden by env or CLI.
DEFAULT_CONFIG_DIR = Path(
    os.environ.get(
        "ICHAVA_DEV_CONFIG_DIR",
        str(Path(__file__).resolve().parents[3] / "config"),
    )
)


class SourceConfig(BaseModel):
    """Where to fetch upstream artefacts from."""

    model_config = ConfigDict(extra="allow")

    type: Literal["npm", "github-archive", "github-tag", "github-release", "url", "script"]
    """Strategy id; the orchestrator dispatches on this."""

    package: str | None = None
    """npm package id (type=npm)."""

    owner: str | None = None
    repo: str | None = None
    """GitHub coordinates (type=github-*)."""

    archive_url: str | None = None
    """Templated download URL (type=github-archive). Supports `{version}`."""

    source_path: str | None = None
    """Subdir under the extracted tree to copy from."""

    path: str | None = None
    """Pack-supplied script to invoke (type=script)."""

    args: list[str] | None = None
    """CLI args to pass to the script (type=script). `{version}` is interpolated."""


class SinkConfig(BaseModel):
    """Where the refreshed assets land."""

    model_config = ConfigDict(extra="allow")

    type: Literal["filesystem", "git-branch"]
    root: str | None = None
    """Filesystem destination root. `{pack_root}` is interpolated."""

    repo_root: str | None = None
    """For git-branch: the pack repo to commit into."""

    branch: str | None = None
    """For git-branch: branch name. `{version}` is interpolated."""


class PackConfig(BaseModel):
    """The full per-pack config -- one of these per `config/<slug>.json`."""

    model_config = ConfigDict(extra="forbid")

    @model_validator(mode="before")
    @classmethod
    def _drop_comment_keys(cls, data: object) -> object:
        """JSON has no comments; underscore-prefixed keys (`_note`, ...) are
        authoring annotations, not config -- drop them before validation."""
        if isinstance(data, dict):
            return {k: v for k, v in data.items() if not str(k).startswith("_")}
        return data

    name: str
    """Human-readable pack name (e.g. "twemoji")."""

    pack: str
    """Composer package name (e.g. "ichava/emoji-sets")."""

    pack_root: str
    """Path to the pack repo, relative to the config dir or absolute."""

    current_version: str | None = None
    """Fallback only. The authoritative record is the pack repo's own config --
    see `version_file` and `resolved_current_version`. This field is read when a
    pack has no vendored config yet, and is otherwise ignored."""

    version_file: str = "resources/assets/svg/config.json"
    """Pack-repo-relative JSON file that records the vendored upstream version.
    This travels with the assets, so the version and the SVGs it describes move
    in one commit and the sync converges (V49)."""

    version_keys: list[str] = Field(
        default_factory=lambda: ["upstream.current_version", "package.upstream_version"]
    )
    """Dot-paths inside `version_file` that hold the vendored version. All are
    read (first hit wins) and all are written."""

    version_check_url: str
    """URL the orchestrator GETs to discover the latest version."""

    source: SourceConfig
    sinks: list[SinkConfig] = Field(default_factory=list)
    transforms: list[dict[str, Any]] = Field(default_factory=list)
    """Free-form per-transform config; each entry must have a `type` key."""

    @field_validator("pack_root")
    @classmethod
    def _expand_pack_root(cls, v: str) -> str:
        return os.path.expanduser(v)


class PacksRegistry(BaseModel):
    """The contents of `config/packs.json` -- the master pack list."""

    model_config = ConfigDict(extra="forbid")

    packs: list[Annotated[str, Field(min_length=1)]]
    """Pack slugs (each maps to `<config_dir>/<slug>.json`)."""


def load_registry(config_dir: Path | None = None) -> PacksRegistry:
    """Read `config/packs.json` and return the validated registry."""
    config_dir = config_dir or DEFAULT_CONFIG_DIR
    registry_file = config_dir / "packs.json"
    if not registry_file.is_file():
        raise FileNotFoundError(f"missing pack registry: {registry_file}")
    return PacksRegistry.model_validate_json(registry_file.read_text(encoding="utf-8"))


def load_pack(slug: str, config_dir: Path | None = None) -> PackConfig:
    """Read one pack config and return the validated PackConfig."""
    config_dir = config_dir or DEFAULT_CONFIG_DIR
    pack_file = config_dir / f"{slug}.json"
    if not pack_file.is_file():
        raise FileNotFoundError(f"missing pack config: {pack_file}")
    return PackConfig.model_validate_json(pack_file.read_text(encoding="utf-8"))


def load_all(config_dir: Path | None = None) -> list[PackConfig]:
    """Load every pack listed in the registry, in registry order."""
    registry = load_registry(config_dir)
    return [load_pack(slug, config_dir) for slug in registry.packs]


def interpolate(template: str, **bindings: str) -> str:
    """Replace `{key}` placeholders in `template` with values from `bindings`."""
    out = template
    for k, v in bindings.items():
        out = out.replace("{" + k + "}", str(v))
    return out


def write_pack_config(pack: PackConfig, config_dir: Path | None = None) -> Path:
    """Round-trip a PackConfig back to disk (used after current_version bumps).

    Preserves the user's chosen filename: ``<config_dir>/<name>.json``.
    """
    config_dir = config_dir or DEFAULT_CONFIG_DIR
    target = config_dir / f"{pack.name}.json"
    target.write_text(
        json.dumps(pack.model_dump(exclude_none=True), indent=2) + "\n",
        encoding="utf-8",
    )
    return target


def vendored_version_file(pack: PackConfig) -> Path:
    """Absolute path to the pack repo's own version record."""
    return Path(pack.pack_root) / pack.version_file


def read_vendored_version(pack: PackConfig) -> str | None:
    """Read the version the pack repo says it vendors.

    Returns None when the file, or every declared key, is absent -- callers fall
    back to `pack.current_version`.
    """
    target = vendored_version_file(pack)
    if not target.is_file():
        return None
    try:
        data = json.loads(target.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        return None

    for dotted in pack.version_keys:
        cursor: Any = data
        for seg in dotted.split("."):
            if not isinstance(cursor, dict) or seg not in cursor:
                cursor = None
                break
            cursor = cursor[seg]
        if isinstance(cursor, str) and cursor.strip():
            return cursor.strip()
    return None


def resolved_current_version(pack: PackConfig) -> str | None:
    """The version to compare upstream against.

    The pack repo wins. `current_version` in the toolkit config is a fallback
    for a pack that has no vendored record yet: it lives in this repository, so
    a bump written during a sync run is discarded with the runner (V49).
    """
    return read_vendored_version(pack) or pack.current_version


def write_vendored_version(pack: PackConfig, version: str) -> Path | None:
    """Record `version` in the pack repo, at every declared key.

    Returns the file written, or None when the pack ships no such file. Writing
    into the pack repo is what makes the sync converge: the GitBranch sink
    commits it alongside the assets, so the next run compares against the
    version that actually shipped.
    """
    target = vendored_version_file(pack)
    if not target.is_file():
        return None

    data = json.loads(target.read_text(encoding="utf-8"))
    changed = False

    for dotted in pack.version_keys:
        segs = dotted.split(".")
        cursor: Any = data
        for seg in segs[:-1]:
            if not isinstance(cursor, dict) or seg not in cursor:
                cursor = None
                break
            cursor = cursor[seg]
        if isinstance(cursor, dict) and segs[-1] in cursor and cursor[segs[-1]] != version:
            cursor[segs[-1]] = version
            changed = True

    if changed:
        target.write_text(json.dumps(data, indent=2) + "\n", encoding="utf-8")
    return target
