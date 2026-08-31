"""Single tenacity-wrapped HTTP client used by every Source.

Centralising HTTP lets us:

- apply one retry policy across registries (npm/github/packagist) so the
  whole tool rides out a 503 without each source duplicating tenacity
- inject one User-Agent string CI / abuse teams can identify
- swap requests for httpx later without touching call sites

Public surface:

    >>> from ichava_maintainer_toolkit.core.http import get_json, download
    >>> data = get_json("https://registry.npmjs.org/@x/y/latest")
    >>> path = download("https://github.com/.../v1.zip", Path("./tmp/v1.zip"))
"""

from __future__ import annotations

import logging
from pathlib import Path
from typing import Any

import requests
from tenacity import retry, retry_if_exception_type, stop_after_attempt, wait_exponential

logger = logging.getLogger(__name__)

USER_AGENT = "ichava-maintainer-toolkit (+https://github.com/ichava/maintainer-toolkit)"
DEFAULT_TIMEOUT = 30
DEFAULT_DOWNLOAD_TIMEOUT = 300

_session = requests.Session()
_session.headers.update({"User-Agent": USER_AGENT, "Accept": "application/json"})


@retry(
    stop=stop_after_attempt(4),
    wait=wait_exponential(multiplier=2, min=2, max=30),
    retry=retry_if_exception_type((requests.RequestException, requests.HTTPError)),
    reraise=True,
)
def get_json(url: str, *, timeout: int = DEFAULT_TIMEOUT) -> Any:
    """GET a JSON endpoint with retries. Raises on non-2xx after 4 attempts."""
    logger.debug("GET %s", url)
    response = _session.get(url, timeout=timeout)
    response.raise_for_status()
    return response.json()


@retry(
    stop=stop_after_attempt(4),
    wait=wait_exponential(multiplier=2, min=2, max=60),
    retry=retry_if_exception_type((requests.RequestException, requests.HTTPError)),
    reraise=True,
)
def download(
    url: str, dest: Path, *, force: bool = False, timeout: int = DEFAULT_DOWNLOAD_TIMEOUT
) -> Path:
    """Stream a binary download to disk. Idempotent: skips if already cached."""
    dest.parent.mkdir(parents=True, exist_ok=True)
    if dest.exists() and dest.stat().st_size > 0 and not force:
        logger.debug("cached: %s", dest)
        return dest

    logger.info("downloading %s -> %s", url, dest)
    response = _session.get(url, stream=True, timeout=timeout)
    response.raise_for_status()

    tmp = dest.with_suffix(dest.suffix + ".tmp")
    with tmp.open("wb") as fh:
        for chunk in response.iter_content(chunk_size=1 << 16):
            fh.write(chunk)
    tmp.rename(dest)
    return dest
