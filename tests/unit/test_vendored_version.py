"""V49 -- the pack repo is the source of truth for the version it vendors.

The sync used to compare upstream against `current_version` in this
repository's `config/<pack>.json`, and to write the bump back through the same
path. Under Docker that path is the runner's throwaway checkout, deleted by the
workflow's cleanup step, so the recorded version never moved and every
scheduled run re-opened the pull request it had already opened.
"""

from __future__ import annotations

import json

import pytest

from ichava_maintainer_toolkit.core.checker import check_pack
from ichava_maintainer_toolkit.core.config import (
    PackConfig,
    read_vendored_version,
    resolved_current_version,
    write_vendored_version,
)


def make_pack(tmp_path, *, toolkit_version="3.0.0", vendored=None):
    """A pack whose repo optionally carries its own version record."""
    if vendored is not None:
        target = tmp_path / "resources" / "assets" / "svg"
        target.mkdir(parents=True)
        (target / "config.json").write_text(
            json.dumps(
                {
                    "package": {"version": "0.1.0", "upstream_version": vendored},
                    "upstream": {"current_version": vendored},
                }
            )
        )
    return PackConfig(
        name="tabler-icons",
        pack="ichava/tabler-icons",
        pack_root=str(tmp_path),
        current_version=toolkit_version,
        version_check_url="https://registry.npmjs.org/@tabler/icons/latest",
        source={"type": "npm", "package": "@tabler/icons"},
    )


def test_reads_the_version_the_pack_repo_records(tmp_path):
    pack = make_pack(tmp_path, vendored="3.46.0")

    assert read_vendored_version(pack) == "3.46.0"


def test_falls_back_to_the_toolkit_config_when_the_pack_has_no_record(tmp_path):
    pack = make_pack(tmp_path, toolkit_version="3.0.0", vendored=None)

    assert read_vendored_version(pack) is None
    assert resolved_current_version(pack) == "3.0.0"


def test_the_pack_repo_wins_over_the_toolkit_config(tmp_path):
    pack = make_pack(tmp_path, toolkit_version="3.0.0", vendored="3.46.0")

    assert resolved_current_version(pack) == "3.46.0"


def test_writing_updates_every_declared_key(tmp_path):
    pack = make_pack(tmp_path, vendored="3.0.0")

    written = write_vendored_version(pack, "3.46.0")

    assert written is not None
    data = json.loads(written.read_text())
    assert data["upstream"]["current_version"] == "3.46.0"
    assert data["package"]["upstream_version"] == "3.46.0"
    # Only the declared keys move: the package's own release version is not an
    # upstream version and must not be rewritten.
    assert data["package"]["version"] == "0.1.0"


def test_writing_is_a_no_op_when_the_pack_ships_no_record(tmp_path):
    pack = make_pack(tmp_path, vendored=None)

    assert write_vendored_version(pack, "3.46.0") is None


def test_the_sync_converges(tmp_path, monkeypatch):
    """The regression this whole change exists for.

    Upstream is 3.46.0 and the pack already vendors 3.46.0, while the toolkit
    config still says 3.0.0. Before the fix this reported stale and re-opened
    the same PR every week.
    """
    monkeypatch.setattr(
        "ichava_maintainer_toolkit.core.checker.get_json",
        lambda url: {"version": "3.46.0"},
    )
    pack = make_pack(tmp_path, toolkit_version="3.0.0", vendored="3.46.0")

    result = check_pack(pack)

    assert result.current == "3.46.0"
    assert result.latest == "3.46.0"
    assert result.stale is False


def test_still_detects_a_genuine_upstream_bump(tmp_path, monkeypatch):
    monkeypatch.setattr(
        "ichava_maintainer_toolkit.core.checker.get_json",
        lambda url: {"version": "3.47.0"},
    )
    pack = make_pack(tmp_path, toolkit_version="3.0.0", vendored="3.46.0")

    result = check_pack(pack)

    assert result.current == "3.46.0"
    assert result.stale is True


@pytest.mark.parametrize("reason_url", ["", None])
def test_a_pack_without_a_check_url_still_reports_the_vendored_version(tmp_path, reason_url):
    pack = make_pack(tmp_path, vendored="3.46.0")
    pack = pack.model_copy(update={"version_check_url": reason_url or ""})

    result = check_pack(pack)

    assert result.current == "3.46.0"
    assert result.reason == "no version_check_url"
