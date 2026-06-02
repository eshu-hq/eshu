# storageeval Agent Rules

This package defines pure storage migration proof contracts. It must stay
adapter-free, queue-free, and runtime-free.

## Mandatory rules

- MUST keep this package standard-library-only.
- MUST NOT open Postgres, call NornicDB, write graph state, enqueue reducer
  work, expose API/MCP routes, or decide canonical truth.
- MUST require failing tests before changing comparison labels, validation
  behavior, or exported evidence fields.
- MUST reject missing, stale, divergent, truncated, unsupported, unbounded, or
  fallback-truth shadow evidence.
- MUST keep production fallback behavior explicit in every passing comparison.
- MUST keep fact-write rollback behavior explicit and disposable; do not add
  queue, lease, retry, or dead-letter ownership here.

## Read first

1. `README.md` for the package boundary and invariants.
2. `doc.go` for the godoc contract.
3. `shadow_read.go` before changing the #1287 comparison gate.
4. `shadow_write.go` before changing the #1288 comparison gate.
5. `docs/internal/design/431-nornicdb-primary-store-evaluation.md` before
   changing storage migration evidence semantics.

## Verification

Run from the repository root:

```bash
(cd go && go test ./internal/storageeval -count=1)
(cd go && go vet ./internal/storageeval)
(cd go && golangci-lint run ./internal/storageeval)
./scripts/verify-package-docs.sh
git diff --check
```
