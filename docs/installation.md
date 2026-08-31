[← Package README](../README.md#documentation)

# Installation

*How-to guide.*

The toolkit is Docker-first. Every fetch strategy it uses. Python, Node, `gh`, the archive
handlers: is baked into the image, so a maintainer needs Docker and nothing else.

## Build the image

```bash
docker build -t ichava-maintainer-toolkit:local .
```

The CI workflows in the icon packs build from a checkout of this repository rather than pulling a
published image, because the GHCR package is private and another repository's `GITHUB_TOKEN` cannot
pull it.

## Run it

```bash
docker run --rm -v "$PWD/..:/work" ichava-maintainer-toolkit:local list-packs
```

`/work` must be the directory that **contains** the pack repositories, not a pack itself, each
pack's `pack_root` in `config/` is expressed relative to it.

## Running it locally instead

```bash
pip install -e '.[dev]'
ichava-maintainer-toolkit list-packs
```

Useful for working on the toolkit itself. For an actual sync, use the image: it is what CI runs, and
a local Python with a different Node or `gh` version is a different tool.

## Requirements

Python 3.12, and a `GH_TOKEN` with permission to push a branch and open a pull request in the pack
repositories.

---

[← Docs index](../README.md#documentation)
