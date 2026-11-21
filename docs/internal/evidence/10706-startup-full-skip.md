# #10706 startup full skip: no-regression evidence

A restarted winners maintainer unconditionally ran one full resweep over the
92M-row fact table, holding the transaction-scoped advisory single-flight
lock for 80+ minutes (observed stuck 87+ min on QA with zero commits) while
every keyed tick returned skipped and R stayed frozen. This note is the
tracked record the perf-evidence gate requires for the startup gate change
that skips the full when a committed watermark already proves the table was
swept.

## Superseded by the persisted contract (#11117)

The per-process `fullDone` seed described below is replaced by a decision read
from the persisted winners contract on every maintainer tick (#11117): no
watermark row, a NULL or a lower stored contract version runs one guarded full
across every instance, an equal version runs keyed ticks, and a higher version
stands the binary down. A committed watermark alone no longer skips the full,
because it does not say which statements produced it. The stall this note
records (a startup full on every roll holding the single-flight lock) is still
prevented: a roll with an unchanged contract runs no full. See
`11117-winners-contract.md`.

## No-Regression Evidence:

This change adds a startup-only seed check, not per-tick work: when a
`KeyedRebuilder` and a committed watermark exist, `fullDone` starts true and
ticks go keyed immediately; every other shape (nil reader, read error, no
watermark, pure-full maintainer, failed full) keeps the old unconditional
startup full. Baseline: the unconditional startup full reconcile. After:
identical keyed tick path, claim shape, locks, and worker counts — the seed
runs once per restart, outside the tick loop. Backend: hermetic unit legs (no live backend). Input shape: synthetic
watermark states (present/absent/read-error). Terminal counts:
TestWinnersMaintainerSkipsStartupFullWhenWatermarkExists plus the
positive/negative/error triple green; full supplychain/core suite green
(0.8s); `go build`, `go vet`, gofumpt, `git diff --check` clean. Why safe:
fail-closed — a never-swept table still gets its backfill reconcile, and a
failed full still gates keyed ticks until one commits.

## Observability Evidence:

The skip path emits an INFO log naming the committed watermark time
(`skipping startup full resweep: committed watermark exists`). The existing
path-dimensioned resweep metrics plus the watermark gauge already
distinguish "straight to keyed" from "startup full" with no new counter
needed. Live QA proof (R advancing post-deploy) is owned by the post-merge
rollout, not this change.
