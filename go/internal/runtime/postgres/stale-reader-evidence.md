# Stale reader: no-regression evidence (#10416 slice 0)

The `StaleReader` mechanism in this directory adds an opt-in standby
read path with no production caller. This note records why it cannot
regress production: routing defaults to `off`, the stale path is
unreachable unless explicitly wired, and a wiring guard blocks the
one dangerous misuse (fencing stores built from a reader).

## No-Regression Evidence:

- Baseline: new mechanism, no prior production load. `StaleReader()`
  is referenced only by its own tests; no `go/cmd` wiring constructs
  it, so the steady-state statement delta is zero.
- After: identical to baseline. With routing `off` (the default) every
  borrow attempt defers before touching any pool or connection, and the
  off-path borrows nothing. Enabling the path requires both
  `ESHU_POSTGRES_READ_ROUTING=stale-audits` and explicit wiring at a
  call site; neither ships in this slice.
- Backend/version: package unit proof with a fake database driver
  (`go test ./internal/runtime/postgres/`: 178 top-level PASS, 0 FAIL,
  covering serve-fresh, budget-exceeded, not-standby, never-replayed,
  split-brain, routing-off, no-observer, snapshot-set absence, fleet
  failover and all-lagged deferral, fixed error string, config parses,
  guard RED/GREEN including var-declared and nested aliases, OTEL
  closed labels); `go test -race` on the touched packages, exit 0.
- Input shape and counts: no production query changes shape; no
  row-count, retry, backoff, or queue change. The intended behavior
  change is the new mechanism itself, shown by the passing suite, not
  a timing shift.
- Why safe: the fencing borrow path is preserved by an exact split
  (reviewed), the stale store exposes no snapshot-set or Exec surface,
  invalid routing values fail startup, and deferral never falls back
  to the writer (which would return shed load under pressure).

## Observability Evidence:

- New counter `eshu_dp_postgres_reader_stale_deferrals_total`,
  labelled by closed family names only (no SQL, endpoints, or
  credential detail in labels or wire text).
- Family, replay age, and budget travel through `errors.As` for logs,
  never as metric labels.
- Existing reader/writer instruments are untouched; with zero callers
  the new counter can only report zero until a later slice wires one.
