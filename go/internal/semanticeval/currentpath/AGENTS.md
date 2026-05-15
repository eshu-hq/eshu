# Current-Path Semantic Eval Runner

This package belongs to the Phase 0 NornicDB semantic retrieval evaluation
baseline. Keep it as a thin HTTP runner over public Eshu query surfaces.

Rules:

- Do not import graph, storage, reducer, collector, or query handler packages.
- Do not bypass HTTP to read Postgres, NornicDB, or Neo4j directly.
- Keep every request bounded by an explicit mode, limit, and timeout.
- Preserve strict JSON decoding for suite files.
- Add tests before changing request construction, truth mapping, unsupported
  handling, or candidate handle normalization.
