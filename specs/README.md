# Specs

`specs/` holds machine-readable contracts that are consumed by tests and
documentation.

- `capability-matrix.v1.yaml` plus `capability-matrix/*.yaml` define
  user-facing capability behavior by runtime profile.
- `capability-catalog.v1.yaml` is the editorial overlay for the capability
  catalog: display names, owner packages, maturity overrides, known gaps, linked
  issues, docs, and the exemption/non-MCP-surface lists. It is reconciled with
  the matrix and the live MCP registry by `go/cmd/capability-inventory` into
  `go/internal/capabilitycatalog/data/catalog.generated.json`. See
  `docs/public/reference/capability-catalog.md`.
- `authorization-catalog.v1.yaml` defines the v1 built-in roles, explicit
  action grants, data classes, permission families, bootstrap-owner posture, and
  custom-policy deferral that enrich every generated capability catalog entry.
  See `docs/public/reference/authorization-catalog.md`.
- `backend-conformance.v1.yaml` defines graph-backend capability classes for
  official adapters.
- `scale-lab-corpus.v1.yaml` defines the representative scale-lab corpus,
  privacy, metric, and threshold contract that gates reducer, graph-write,
  API/MCP, and correlation fanout scale work. See
  `docs/public/reference/local-testing/representative-corpus-suite.md`.
- `scale-benchmark-artifact.v1.yaml` defines the public-safe benchmark result
  artifact required for large-corpus ingestion, reducer drain, graph-write,
  API, MCP, retry/dead-letter, memory, backend, and before/after proof. See
  `docs/public/reference/local-testing/scale-benchmark-artifact.md`.

Treat edits here as contract changes. Update matching docs and verification in
the same PR.
