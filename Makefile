# ichava/maintainer-toolkit — maintainer toolkit Makefile.
#
# Every target shells out to docker compose so the local host doesn't
# need Python, Node, gh, or any of the system deps. Run `make help`.

SHELL := /usr/bin/env bash
.DEFAULT_GOAL := help

# Pack identifier for single-pack targets. Override:
#   make sync PACK=emoji-sets
PACK ?=

DC := docker compose
RUN := $(DC) run --rm

# ---------------------------------------------------------------------------
# Help (auto-generated from `## ` comments after target names)
# ---------------------------------------------------------------------------

.PHONY: help
help:  ## Show this help
	@printf "\nichava/maintainer-toolkit — maintainer toolkit\n\n"
	@printf "Usage: make <target> [PACK=<name>]\n\n"
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) \
		| awk 'BEGIN{FS=":.*?## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'
	@printf "\n"

# ---------------------------------------------------------------------------
# Image lifecycle
# ---------------------------------------------------------------------------

.PHONY: build
build:  ## Build the docker image (run once + after Dockerfile changes)
	$(DC) build

.PHONY: pull
pull:  ## Pull the latest base image
	$(DC) pull

.PHONY: shell
shell:  ## Drop into a bash shell inside the container
	$(RUN) --entrypoint bash ichava-maintainer-toolkit

# ---------------------------------------------------------------------------
# The actual work
# ---------------------------------------------------------------------------

.PHONY: menu
menu:  ## Open the interactive menu (default action)
	$(RUN) ichava-maintainer-toolkit menu

.PHONY: check
check:  ## Check upstream status for every configured pack (no writes)
	$(RUN) ichava-maintainer-toolkit check

.PHONY: check-pack
check-pack:  ## Check upstream status for one pack: PACK=<name>
	@if [ -z "$(PACK)" ]; then echo "PACK= is required (e.g. make check-pack PACK=emoji-sets)"; exit 2; fi
	$(RUN) ichava-maintainer-toolkit check --pack=$(PACK)

.PHONY: sync
sync:  ## Sync upstream for one pack (refresh + commit + PR): PACK=<name>
	@if [ -z "$(PACK)" ]; then echo "PACK= is required (e.g. make sync PACK=emoji-sets)"; exit 2; fi
	$(RUN) ichava-maintainer-toolkit sync --pack=$(PACK)

.PHONY: sync-all
sync-all:  ## Sync upstream for every pack (sequential, dry-run by default)
	$(RUN) ichava-maintainer-toolkit sync --all --dry-run

.PHONY: sync-all-force
sync-all-force:  ## Sync every pack for real (skip dry-run gate). Be careful.
	$(RUN) ichava-maintainer-toolkit sync --all

.PHONY: build-emoji
build-emoji:  ## Run the emoji-sets ETL recipe (Twemoji + OpenMoji + CLDR)
	$(RUN) ichava-maintainer-toolkit recipe emoji-sets

# ---------------------------------------------------------------------------
# Dev workflow
# ---------------------------------------------------------------------------

.PHONY: test
test:  ## Run the pytest suite inside the container
	$(RUN) --entrypoint pytest ichava-maintainer-toolkit -q

.PHONY: lint
lint:  ## Run ruff
	$(RUN) --entrypoint ruff ichava-maintainer-toolkit check src tests

.PHONY: typecheck
typecheck:  ## Run mypy
	$(RUN) --entrypoint mypy ichava-maintainer-toolkit src

.PHONY: format
format:  ## Auto-format with ruff
	$(RUN) --entrypoint ruff ichava-maintainer-toolkit format src tests

# ---------------------------------------------------------------------------
# House-keeping
# ---------------------------------------------------------------------------

.PHONY: clean
clean:  ## Remove generated caches + temp dirs
	rm -rf .cache .downloads __pycache__ .pytest_cache .mypy_cache .ruff_cache
	find . -type d -name '__pycache__' -prune -exec rm -rf {} +

.PHONY: nuke
nuke:  ## Drop the docker volumes (npm cache, pip cache). Forces a cold rebuild.
	$(DC) down -v
