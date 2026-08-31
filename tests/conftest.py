"""Shared pytest fixtures."""

from __future__ import annotations

from pathlib import Path

import pytest


@pytest.fixture
def tmp_pack_root(tmp_path: Path) -> Path:
    """A throwaway pack root with the canonical assets layout."""
    root = tmp_path / "pack"
    (root / "resources" / "assets" / "svg" / "files").mkdir(parents=True)
    (root / "CHANGELOG.md").write_text(
        "# Changelog\n\nAll notable changes follow Keep a Changelog.\n",
        encoding="utf-8",
    )
    return root
