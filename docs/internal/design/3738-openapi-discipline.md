# X2: OpenAPI surface discipline

**ADR**: docs/internal/design/3738-openapi-discipline.md
**Epic**: #3738
**Leaf issues**: #3781 (verifier), #3782 (regenerator, optional), #3783 (CI gate)

## Problem

The Eshu query API serves 200+ HTTP routes registered via `mux.HandleFunc` in
`go/internal/query/` and `go/internal/serviceintelhttp/`. Each route must have a
matching OpenAPI 3.0 fragment in `go/internal/query/openapi_paths_*.go`. Today
there is no automated check that the two surfaces agree. Drift between registered
routes and documented paths can accumulate silently.

## Decision

Add a self-contained bash verifier (`scripts/verify-openapi.sh`) that extracts
both sets — HandleFunc routes and OpenAPI path entries — and exits non-zero on
any drift. Wire the verifier into CI.

### Verifier design

**Input surfaces:**
- HandleFunc registrations in `go/internal/query/*.go` and
  `go/internal/serviceintelhttp/*.go` (excluding `*_test.go` and `openapi_*.go`)
- OpenAPI path definitions in `go/internal/query/openapi_paths_*.go`

**Three HandleFunc patterns are handled:**

1. Direct string literal: `mux.HandleFunc("GET /path", ...)`
2. Route constant reference: `const r = "GET /path"; mux.HandleFunc(r, ...)`
3. String concatenation: `const p = "/path"; mux.HandleFunc("POST "+p, ...)`

**OpenAPI extraction** uses an awk depth-aware parser that handles paths with
multiple HTTP methods (GET + POST on the same endpoint).

**Exit codes:**
- `0` — HandleFunc routes and OpenAPI entries are identical (clean)
- `1` — drift detected (missing or orphan entries reported)

### Regenerator (optional)

A Go tool at `cmd/openapi-generator/main.go` may walk HandleFunc registrations
and emit `openapi_paths_*.go` files. It is optional and not wired into the
default build.

### CI gate

The verifier runs on every PR and push to `main` via
`.github/workflows/verify-openapi.yml`. A red gate blocks merge until the drift
is resolved.

## Consequences

- Every new or changed API route must update its `openapi_paths_*.go` fragment
  before the CI gate passes.
- V-1 (#3813, `/api/v1/` alias) must land AFTER X2-1 is merged so the verifier
  gates V-1's OpenAPI changes correctly.
- Known intentional exceptions (`/docs`, `/redoc` — documentation UIs) are
  caught by the verifier as drift. They are not exempted; the verifier forces an
  explicit decision.
- The verifier is self-contained (bash + rg + awk), requiring no Go compilation
  or external dependencies beyond what is already present in CI.

## Verification

```bash
# Unit tests for the verifier (synthetic fixtures)
bash scripts/test-verify-openapi.sh

# Full verifier on the current tree
bash scripts/verify-openapi.sh
```
