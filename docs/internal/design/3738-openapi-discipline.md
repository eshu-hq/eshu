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

### Known-drift exclusions

`.github/openapi-known-drift.txt` lists routes intentionally excluded from the
OpenAPI surface — documentation UIs, pre-existing gaps, and routes whose METHOD
or path is computed at runtime or lives in a package outside the verifier's scan
directories. One route per line in `METHOD /path` format; comments start with
`#`. The verifier subtracts these from the drift report via `comm -23` so the CI
gate stays green on acknowledged gaps while catching new drift.

**Dynamic-route escape hatch:** the verifier resolves route constants and string
concatenation (patterns 1b/1c) via regex, not AST. A route whose METHOD or path
is constructed at runtime (e.g. built from a non-const function return, or whose
const lives in a package the verifier does not scan) cannot be resolved. Such
routes must be added to the known-drift file. Do not weaken the matcher to chase
dynamic routes — the file is the explicit escape hatch.

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
- Routes whose METHOD/path is computed at runtime or lives outside scan dirs
  must be listed in `.github/openapi-known-drift.txt`.

## Verification

```bash
# Unit tests for the verifier (synthetic fixtures)
bash scripts/test-verify-openapi.sh

# Full verifier on the current tree
bash scripts/verify-openapi.sh
```
