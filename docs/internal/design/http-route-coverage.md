# HTTP Route Handler Test Coverage Gate

## Status
Accepted (Epic R #3739)

## Problem
Routes registered via `mux.HandleFunc` in `go/internal/query/` and `go/cmd/api/` have no automated check that a handler test exists. Gap-analysis § P0-6 identified 10 uncovered `/api/v0/*` routes. A PR that adds a new route without a test silently passes CI, leaving the route unvalidated until an integration or production failure.

## Decision
A CI gate (`verify-route-coverage.sh`) fails when a new `HandleFunc` registration has no matching `Test*` function in any `*_test.go` file. The gate is diff-based: only routes on changed files are checked, and pre-existing unknown routes are not flagged.

## Design

### Route-to-test matching
For each `HandleFunc("METHOD /path", h.methodName)` registration in a changed file:

1. Convert `methodName` to PascalCase (e.g. `getRepositoryTree` → `GetRepositoryTree`)
2. Also try without `Get`/`Post`/`Put`/`Delete`/`Handle`/`List` prefix
3. For short method names (< 7 chars PascalCase), add the file-stem PascalCase as fallback
   - `detail` in `fact_schema_version.go` → also search for `FactSchemaVersion`
   - `getFamily` in `collector_extraction_readiness.go` → `Family` (from strip) matches `CollectorExtractionReadinessFamily`
4. Search all `*_test.go` files, case-insensitively, for `func Test\w*<term>\w*(`

A route is covered if any search term matches a test function name.

Matching is case-insensitive because `pascal_case()` title-cases each
snake_case segment of the file stem (`saml_handler` -> `SamlHandler`), but
idiomatic Go test names preserve initialisms as written in the source
identifier (`SAMLHandler`, matching the `SAML` acronym in the handler struct
name). An exact-case search false-positived as "uncovered" on `saml_handler.go:handleACS`
even though `TestSAMLHandlerACSCreatesHashOnlyBrowserSession` (and three
sibling tests) already covered it (#4964).

### Diff scope
- CI (`GITHUB_BASE_REF`): compares against PR base branch
- Local: compares against `origin/main` merge-base or `HEAD~1`
- Only files under `go/internal/query/` and `go/cmd/api/` are checked

### Files
- `scripts/verify-route-coverage.sh` — main verifier
- `scripts/test-verify-route-coverage.sh` — test mirror (3 cases)
- `.github/workflows/verify-route-coverage.yml` — CI workflow

## Consequences
- Any PR that adds a new route without a handler test fails CI
- Pre-existing uncovered routes (e.g. `ServeReDoc`, `handleAsk`) are not retroactively flagged
- The test mirror validates the verifier itself does not regress

## Limitations

The gate proves a similarly-named test function exists — it does not prove the test exercises the route or asserts meaningful behavior. A test named to match the convention but asserting nothing satisfies the gate. This is intentional: the gate is a name-coverage heuristic, not a behavioral-coverage verifier. Operators should treat a green gate as "a test with a plausible name exists," not as "the route is behavior-covered."

For short method names (fewer than 7 characters after prefix stripping), the gate uses a concatenated file-stem+method search term (e.g., `FactSchemaVersion` + `Detail` → `FactSchemaVersionDetail`) to avoid matching unrelated sibling tests in the same file. This prevents false passes from pre-existing tests like `TestRepositoryListCatalog` when a new short method is added to `repository.go`.

## Cross-references
- Epic R #3739: HTTP route coverage
- gap-analysis.md § P0-6: 10 uncovered routes
