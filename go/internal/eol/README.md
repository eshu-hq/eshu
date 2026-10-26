# eol

## Purpose

`go/internal/eol` vendors an endoflife.date snapshot
and joins it against declared runtime versions, so a read path can report
whether a repository's runtime is still supported without any network access
(issue #9163, research in
`docs/internal/evidence/8820-eol-snapshot-research.md`).

One fact is one answer: a declared runtime plus major/minor version resolves
to `supported`, `eol`, `support-ending`, or `unknown`, and every answer
carries the snapshot date, its age in days, and whether the snapshot is past
its refresh window. Missing data is never read as supported.

## Where this fits

```text
refresh job -> snapshots/*.json + snapshot_date -> go:embed -> Resolve
                                                          (read path)
```

`Resolve` is a pure function of the snapshot, the declared runtime and
version, and the query clock. The declaration package
(`go/internal/collector/repo/runtime/declaration`) stays the producer of
`runtime_declaration.version` facts; this package owns the EOL side of the
join and has no collector, reducer, or query imports. It lives directly
under `go/internal` rather than beside the producer because it is a
dependency-free library, not a pipeline stage: nothing here performs
collection, projection, queue work, or graph writes.

## Exported surface

- `Resolve` — one declared version to an `Answer` (`supported`, `eol`,
  `support-ending`, `unknown`) with snapshot age and staleness.
- `ParseSnapshot` — vendored files plus date to a `Snapshot`; fails on
  malformed input rather than vendoring data the join would misread.
- `LoadSnapshot` — the embedded snapshot; the read path calls it once.
- `RefreshWindowDays` (30) — the freshness window behind `Answer.Stale`.

## Contract rules

| Input | Result |
| --- | --- |
| `node` 22, `go` 1.24, rolling `eol: false` | `supported` |
| Past `eol` day | `eol` |
| Past `support`, before `eol` | `support-ending` (no `support` field: `supported` until `eol`) |
| `java`, `swift`, unmapped token, missing cycle | `unknown` |
| Bare major that spans cycles (`python` 3) | `unknown` (ambiguous, never guessed) |
| Range, alias, unresolved specs | `unknown` (callers do not pass version parts for these) |

Dates are inclusive last days: a cycle is `eol` the day after its eol date,
and `support-ending` the day after its support date.

## Evidence

No-Regression Evidence: this package is new, so there is no prior baseline;
the after measurement below is the initial cost record. Backend: linux/arm64,
go1.26.6, `go test -bench` in the package (2026-10-26, two runs, shared
host so figures are bounds not exact): BenchmarkResolve under 1 us/op
against the embedded snapshot (one linear scan over at most the 71 rust
cycles per declared version, no I/O), BenchmarkLoadSnapshot under 1 ms
one-time at startup for 8 products / 196 cycles. Input shape: an in-memory
Snapshot plus (runtime, major, minor) ints. No queue, row, graph, or
storage counts change: no caller wires the join into a read path yet, so
terminal counts are identical before and after; the focused package tests
(17 cases, `go test ./internal/eol/`) pass. Safe because Resolve allocates
only its Answer value, touches no shared state, and never reads missing
data as supported (unknown by construction).

No-Observability-Change: the package performs no projection, queue write,
retry, storage call, graph work, or network I/O and emits no metric, span,
or log of its own; coverage is recorded in
docs/public/observability/telemetry-coverage.md (eol snapshot join row).
When a read path wires the join in, that surface owns its signals.

## Snapshot refresh

`scripts/refresh-eol-snapshot.sh` re-fetches the eight product files, writes
`snapshot_date`, and prints the size for the PR description. The refresh
opens as a normal PR so review sees the data diff. Query code reads only the
vendored files, so offline deployments behave identically modulo staleness.
