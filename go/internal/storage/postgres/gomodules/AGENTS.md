# AGENTS.md - storage/postgres/gomodules guidance

## Read first

1. `README.md`
2. `store.go`, including the measurement on `pageQuery`
3. `go/internal/storage/postgres/AGENTS.md`

## Invariants

- One page examines at most `scannercomponent.GoModulePageSize` candidates. The
  go.mod probe must stay inside that window.
- The page arithmetic matches `scannercomponent.GoModulePageWindow`. Change both
  together and keep the live rotation test green.
- Any change to `pageQuery` needs a fresh `EXPLAIN (ANALYZE, BUFFERS)` on
  representative data, per `eshu-postgres-rigor`.
- This package reads only. Do not add writes or a schema change here.

## Verification

```bash
ESHU_POSTGRES_DSN=... ESHU_POSTGRES_DISPOSABLE=1 go test -count=1 ./internal/storage/postgres/gomodules
```
