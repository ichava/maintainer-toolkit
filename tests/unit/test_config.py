"""Config loader: ensure JSON round-trips and the schema rejects garbage."""

from __future__ import annotations

import json
from pathlib import Path

import pytest

from ichava_maintainer_toolkit.core.config import (
    interpolate,
    load_pack,
    load_registry,
    write_pack_config,
)


def _seed_config_dir(tmp_path: Path, name: str, body: dict) -> Path:
    cfg_dir = tmp_path / "config"
    cfg_dir.mkdir()
    (cfg_dir / "packs.json").write_text(json.dumps({"packs": [name]}), encoding="utf-8")
    (cfg_dir / f"{name}.json").write_text(json.dumps(body), encoding="utf-8")
    return cfg_dir


def test_load_registry_round_trips(tmp_path):
    cfg_dir = _seed_config_dir(
        tmp_path,
        "demo",
        {
            "name": "demo",
            "pack": "vendor/demo",
            "pack_root": str(tmp_path / "demo"),
            "version_check_url": "https://example.com/latest.json",
            "current_version": "1.0.0",
            "source": {"type": "npm", "package": "@x/y"},
            "sinks": [],
        },
    )
    registry = load_registry(cfg_dir)
    assert registry.packs == ["demo"]


def test_load_pack_validates_source_type(tmp_path):
    cfg_dir = _seed_config_dir(
        tmp_path,
        "demo",
        {
            "name": "demo",
            "pack": "vendor/demo",
            "pack_root": str(tmp_path / "demo"),
            "version_check_url": "https://example.com/latest.json",
            "current_version": "1.0.0",
            "source": {"type": "totally-fake"},  # invalid Literal value
            "sinks": [],
        },
    )
    with pytest.raises(Exception, match="source"):
        load_pack("demo", cfg_dir)


def test_write_pack_config_round_trips(tmp_path):
    cfg_dir = _seed_config_dir(
        tmp_path,
        "demo",
        {
            "name": "demo",
            "pack": "vendor/demo",
            "pack_root": str(tmp_path / "demo"),
            "version_check_url": "https://example.com/latest.json",
            "current_version": "1.0.0",
            "source": {"type": "npm", "package": "@x/y"},
            "sinks": [],
        },
    )
    cfg = load_pack("demo", cfg_dir)
    bumped = cfg.model_copy(update={"current_version": "1.1.0"})
    write_pack_config(bumped, cfg_dir)

    reloaded = load_pack("demo", cfg_dir)
    assert reloaded.current_version == "1.1.0"


def test_interpolate_substitutes_tokens():
    assert interpolate("a/{x}/b", x="42") == "a/42/b"
    assert interpolate("{a}-{b}", a="1", b="2") == "1-2"
    assert interpolate("no-tokens", x="42") == "no-tokens"
