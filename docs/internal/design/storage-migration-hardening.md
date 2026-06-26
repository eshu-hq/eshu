# ADR: Storage & Migration Hardening (Epic S2)

**Status**: Accepted
**Epic**: #3742
**Date**: 2026-06-26

## Context

The Postgres storage package (`go/internal/storage/postgres/`) holds 52 SQL
migration files in `schema/data-plane/postgres/` mirrored as Go `const` strings
in a `bootstrapDefinitions` slice. This dual-source pattern creates drift risk:
adding a migration requires both the SQL file and a companion Go constant plus
slice registration. The package also lacks explicit tests for migration ordering
correctness and connection pool exhaustion behavior at MAX_OPEN_CONNS+1.

## SQL Injection Audit (S2-4)

### Method

- **Tool**: gosec v2 (repo standard — `.github/workflows/security-scan.yml`)
- **Rules**: G201 (SQL string formatting), G202 (SQL string concatenation)
- **Scope**: `go/internal/storage/postgres/...` and entire Go module

```bash
gosec -include=G201,G202 ./internal/storage/postgres/...
gosec -include=G201,G202 ./...
```

### Results

- **G201/G202 (SQL injection construction)**: 0 findings (exit 0)
- **Full gosec scan** (`gosec ./internal/storage/postgres/...`): 0 findings (exit 0)
- **Codebase-wide gosec** (`gosec ./...`): 0 findings (exit 0)
- **Call-site count**: 566 `ExecContext`/`QueryContext`/`QueryRowContext` calls
  across 181 non-test source files in `go/internal/storage/postgres/`
- **`fmt.Sprintf` surface**: 23 non-test files invoke `fmt.Sprintf`. All
  reviewed — the two dynamic SQL usages (`aws_freshness_store.go:211,218,248`,
  `incident_freshness_store.go:211`) construct parameter placeholder sequences
  (`$1, $2...$N`), not user data. Remaining `fmt.Sprintf` uses are
  non-SQL (error messages, log formatting, telemetry labels).
- **Commit scanned**: `origin/main` at `50d037f769357d22a95f300df2b3fab154d6ce70`

### Reproducibility

```bash
git checkout origin/main
cd go
rg -c '(\.ExecContext|\.QueryContext|\.QueryRowContext)\b' \
   -g '*.go' -g '!_test.go' internal/storage/postgres/
# → 566 calls across 181 files
rg -c 'fmt\.Sprintf' -g '*.go' -g '!_test.go' internal/storage/postgres/
# → 23 files
gosec -include=G201,G202 -quiet ./internal/storage/postgres/...
# → exit 0
```

### Conclusion

Zero SQL injection vulnerabilities detected. The Postgres package enforces
parameterized queries throughout; the two `fmt.Sprintf`+SQL call sites construct
only database-side parameter placeholder sequences.

## Decisions

1. **Replace Go slice with `//go:embed`** (S2-1): The filesystem becomes the
   sole source of truth. Adding a migration only requires a new `.sql` file.
   The embed is compiled into the binary; no runtime file reads.

2. **Migration ordering test** (S2-2): A unit test asserts that every
   `Definition.Path` filename prefix follows a non-decreasing version
   sequence (`001`, `002`, `003a`, ...). This guards against accidental
   reordering during the embed refactor.

3. **Pool exhaustion test** (S2-3): A concurrency behavioral-spec test verifies
   that a bounded connection pool gracefully blocks callers when full,
   surfacing `context.DeadlineExceeded` rather than panicking or leaking.

4. **Security audit** (S2-4): Confirmed zero SQL injection findings via
   gosec G201/G202 and full gosec scan across the entire codebase.

## Consequences

- **Positive**: Single source of truth for migrations; no manual sync; drift is
  impossible.
- **Positive**: Ordering and pool behavior are now explicitly tested.
- **Neutral**: No observable change to bootstrap behavior when the embed FS
  produces the same ordered set.
- **Rollout**: Operator must ensure the `migrations/` directory is present at
  build time (it is compiled in). No runtime action needed beyond a normal
  restart/bootstrap cycle.
