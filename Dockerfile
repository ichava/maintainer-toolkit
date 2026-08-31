# syntax=docker/dockerfile:1.7
#
# ichava/maintainer-toolkit image. Mounts the parent ichava monorepo at /work so the
# tool can read each pack's config + commit refreshed assets.
#
# Build:    docker build -t ichava/maintainer-toolkit .
# Run:      docker run --rm -v "$PWD/..:/work" -w /work/dev ichava/maintainer-toolkit menu
# Compose:  see docker-compose.yml + Makefile.

FROM python:3.12-slim AS base

ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    PIP_NO_CACHE_DIR=1 \
    PIP_DISABLE_PIP_VERSION_CHECK=1

# System deps:
#   git, curl  -- pulling release archives & operating on pack repos
#   nodejs/npm -- the `npm pack <pkg>@<version>` source strategy
#   gh         -- opening PRs against pack repos
#   ca-certs   -- TLS for upstream registries
# (jq was dropped in 0.1.1 -- the orchestrator handles JSON in Python.)
RUN apt-get update \
 && apt-get install -y --no-install-recommends \
        git curl ca-certificates \
        nodejs npm \
 && curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg \
        | dd of=/usr/share/keyrings/githubcli-archive-keyring.gpg \
 && chmod go+r /usr/share/keyrings/githubcli-archive-keyring.gpg \
 && echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" \
        > /etc/apt/sources.list.d/github-cli.list \
 && apt-get update \
 && apt-get install -y --no-install-recommends gh \
 && rm -rf /var/lib/apt/lists/*

WORKDIR /app

# Copy only what's needed for the editable install first to maximise
# Docker layer cache. Then copy the rest of the source.
COPY pyproject.toml README.md ./
COPY src ./src
RUN pip install -e .

# Default config bundle ships in the image; runtime mounts can override
# /app/config or pass --config-dir.
COPY config ./config

# Default working dir is /work so volume mounts of the monorepo land naturally.
WORKDIR /work

ENTRYPOINT ["ichava-maintainer-toolkit"]
CMD ["menu"]
