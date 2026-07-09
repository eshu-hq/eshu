# replay/concurrentreplay

Thread-safe wrapper for the Ifá P2 concurrent replay driver (design doc 4102,
issue #4395, parent epic #4389).

## Purpose

`cassette.Source` (and other replay flavors like it) is single-threaded by
design: it is meant to be driven by one `collector.Service` poll loop, and its
internal scope cursor is unsynchronized. The Ifá P2 driver needs the same
recorded tape drained by N concurrent workers feeding
`ingestion -> fact_work_items -> reducer`. `Source` closes that gap: it wraps
any `collector.Source` delegate behind a mutex and adds a one-shot drain latch,
so the tape is delivered to concurrent callers safely and exactly once.
`Driver` is the concurrent consumer of that wrapped `Source`: it runs a
configurable number of worker goroutines, each looping `Source.Next` then
`Committer.CommitScopeGeneration`, and fails fast — canceling the other
workers — on the first error from either step.

This package is net-new infrastructure. It wraps a delegate and drives it
concurrently; it does not implement its own replay format, does not build the
`fact_work_items` fan-out, and does not construct the reducer drain harness —
those are later slices of #4395.

## Why the delegate call is held under the lock

`cassette.Source.Next` performs no I/O and no network call; it reads an
already-parsed, in-memory `cassette.File` and advances an in-process cursor.
Holding the lock across the delegate call therefore serializes only the
cheap, in-memory tape-cursor advance — not the expensive work. The commit
stage (persisting the collected generation's facts) happens outside this lock,
once per caller, after `Next` returns. Tape handout is inherently sequential —
one cursor over one recorded file — the same property `inputtape.RoundTripper`
(map mutation) and `schedulereplay.ScheduledWorkSource` (schedule cursor) both
serialize for the same reason.

## The one-shot drain latch

`cassette.Source.Next` has poll-restart semantics: once all scopes are
delivered it returns `ok=false` on the following call, then **resets its
internal cursor to zero and replays from the first scope again** on the call
after that — the deliberate behavior a single production poll loop wants
(wait for the next poll interval, then run the batch again). Under concurrent
callers this restart is a correctness hazard: a second wave of callers could
observe a live delegate and re-drain the same recorded generations that a
first wave already consumed.

`Source` closes over this with a `done` latch: the first time the delegate
reports `ok=false` or a non-nil error, `Source` sets `done=true` permanently.
Every subsequent `Next` call — from any goroutine, for the rest of the
wrapper's lifetime — short-circuits to `ok=false, err=nil` without invoking the
delegate again. The delegate's poll-restart never fires because `Source` never
lets the delegate see another call once it has reported exhaustion.

An error from the delegate is treated the same way: it surfaces exactly once,
on the call that observed it, and every later call returns drained rather than
re-invoking a delegate that has already failed.

## Ownership boundary

- `Source` speaks only the top-level `collector.Source` /
  `collector.CollectedGeneration` contract. It has no dependency on any
  specific replay flavor (`cassette`, `schedulereplay`, `parserfixture`, ...);
  any `collector.Source` implementation can be wrapped.
- `Driver` speaks only the top-level `collector.Committer` contract for the
  commit side. It has no dependency on any specific committer implementation
  (Postgres-backed, in-memory, or otherwise).
- This package MUST NOT import `internal/ifa`. The Ifá contract/fixture-pack
  system is a consumer of replayed facts, not a dependency of the replay
  plumbing.
- No network calls, no credentials, no graph backend. Wrapping does not change
  what the delegate does — it only changes who is allowed to call it and when.
  `Driver` does not change what the committer does either; it only runs more
  callers of it concurrently against one shared `Source`.
- `Driver` does not reduce its worker count in response to contention or
  errors. Fewer workers as a stand-in for fixing a non-idempotent commit path
  is the repository's Serialization-Is-Not-A-Fix anti-pattern, not a valid
  Driver behavior.

## Verifying a change

```bash
export GOCACHE="$(git rev-parse --show-toplevel)/.gocache"
cd go && go test -race ./internal/replay/concurrentreplay/... -count=1
```

The concurrency proof (`TestSourceConcurrentNextDeliversEachGenerationExactlyOnce`)
must run under `-race` and must fail without the mutex — that is the point of
this package.

## No-Regression Evidence

This package is additive replay infrastructure. It wraps `collector.Source`
and drives it with `Driver`; it does not modify `cassette.Source`,
`collector.Service`, `collector.Committer` implementations, or any other
existing package. The shared mutable state is: `Source`'s own
`mu`/`done`/`served` fields, guarded end-to-end by one mutex held across each
`Next` call, and `Driver.Run`'s own committed-count counter (accessed only via
`sync/atomic`) and first-error latch (guarded by a `sync.Once`). Verified by
`go test -race ./internal/replay/concurrentreplay/... -count=1`.

## No-Observability-Change

No new metric instruments are minted by this package. `Driver.Instruments`
accepts the existing `*telemetry.Instruments` type but does not yet record
through it — threading it is deferred to a later slice, once it is decided
which existing `eshu_dp_*` instrument applies to a driver invocation.
`Driver.Logger` is optional structured logging only (start, drain, and error
records at Info/Error level via the standard `log/slog` package) — not a new
metric or span. `Source` still returns `collector.CollectedGeneration` values
unchanged from the delegate; the collector's existing telemetry (wired
through `collector.Service`) is unaffected by this package.
