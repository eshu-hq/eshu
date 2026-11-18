# R6 convergence proof: performance and observability evidence

Lane: NVD baseline R6 convergence proof (Refs #10728). One new live
test plus its readiness-inventory enrollment; no production code
changed.

## Performance Evidence:

- Baseline: idempotent re-run was argued, not proven — the only prior
  demonstration was a deleted throwaway test.
- After: TestNVDBaselineTwoPassesConvergeToOneRowSetLive proves two
  baseline passes over one synthetic year-chunk input (10 advisories, 13
  records, fixed timestamps, injected clock) converge to one fact row
  set: byte-identical row sets, zero new captures on the resweep,
  findings 5->5, pending 6->6, 4.00 facts/record, dirty-log 0.30
  rows/fact. The 10-advisory load profile matches the pacing doc's
  R6 scope (single chunk, not a corpus benchmark).
- Backend: Apple M5 Max (darwin/arm64), Homebrew Go toolchain; functional
  proof also on disposable postgres:18-alpine. CI runs the same test on
  linux/amd64.
- Input shape: fixed synthetic chunk, deterministic timestamps, no
  sleeps; whole storage/postgres package suite green (unit + live),
  race detector clean.
- Why the change is safe: test code only, no production query, index,
  lock, pacing, or retry changed; the test exercises the existing
  commit-then-reduce-then-resweep path and pins its idempotency, so it
  cannot regress a p95 — it guards one.

## Observability Evidence:

- No new signals: the proof uses the existing commit/reduce/resweep
  path and its existing instruments. No new metric, label, span, or log
  key is registered.
- The test is enrolled in the live-postgres-readiness inventory (part3
  module + part5 fragment, verified by the drift gate) and selected by
  the reducer contention gate's `-run` filter, so both live lanes
  execute it.
