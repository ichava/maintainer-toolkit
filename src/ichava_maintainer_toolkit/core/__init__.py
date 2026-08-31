"""Core primitives for the ichava-maintainer-toolkit pipeline.

Layout:

* ``pipeline``    -- the Pipeline / Stage / StageContext primitives
* ``config``      -- JSON config loader + Pydantic schema
* ``http``        -- single tenacity-wrapped HTTP client
* ``git``         -- git ops (commit / push / open PR)
* ``checker``     -- upstream version-check (PHP IconPackUpdateChecker parity)
* ``sources/``    -- "where do the SVGs come from" strategies
* ``transforms/`` -- "how do we shape them" strategies
* ``sinks/``      -- "where do they land" strategies
* ``reporters/``  -- "how do we report progress" strategies
"""
