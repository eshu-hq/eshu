# Claim-plane decode validation: no-regression evidence (#9376)

The fail-closed decode change adds one `Validate()` string switch per
decoded closed enum on the claim-plane response path. This note records
why that cost cannot regress the claim plane.

## No-Regression Evidence:

- Baseline: pre-change converter body (plain casts, no validation),
  inlined into a throwaway benchmark on the identical input shape (one
  `claimPlaneWireRun` with valid trigger and status): 2.7 ns/op.
- After: landed `wireRunToRun` with trigger and status validation on the
  same input: 15.0 ns/op. Delta: +12 ns per decoded run.
- Backend/version: shared host linux/arm64, toolchain go1.26.6, module
  go 1.26.10, `go test -bench BenchmarkWireRunToRun -benchtime 2s
  -count 3` (3 stable runs each, 19.8 s wall). Throwaway benchmark file
  removed after measurement; only this note is committed.
- Input shape and counts: the converter runs once per decoded run on the
  read path (`GetRunStatus`, `ListRuns`); no row-count or terminal-count
  change, no retry, backoff, or queue change.
- Why safe: +12 ns sits six orders of magnitude below the operation it
  rides on — a claim-plane HTTP round trip budgeted at 10 s per request
  (`RequestTimeout`). The intended behavior change is the fail-closed
  `invalid` itself, proved by
  `TestClaimPlaneClientNewestServerUnknownEnumValue`, not a timing shift.

## No-Observability-Change:

No new metrics, spans, or logs. The existing claim-plane request observer
(operation, outcome, duration) is untouched. Known characteristic: the
validation error returns after `post` already recorded its outcome, so a
rejected decode reads as transport-OK in request metrics while the caller
sees the `invalid` error. A rejected decode is a protocol mismatch, not a
fence event, so it never carries `ErrClaimRejected`.
