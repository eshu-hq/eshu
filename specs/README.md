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
- `backend-conformance.v1.yaml` defines graph-backend capability classes for
  official adapters.

Treat edits here as contract changes. Update matching docs and verification in
the same PR.
