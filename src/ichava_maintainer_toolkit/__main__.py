"""``python -m ichava_maintainer_toolkit`` entrypoint -- delegates to the Typer CLI."""

from ichava_maintainer_toolkit.cli import app

if __name__ == "__main__":
    app()
