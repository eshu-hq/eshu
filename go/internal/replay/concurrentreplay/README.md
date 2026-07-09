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

This package is net-new infrastructure. It wraps a delegate; it does not
implement its own replay format, does not build the `fact_work_items` fan-out,
and does not construct the reducer drain harness — those are later slices of
#4395.

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
- This package MUST NOT import `internal/ifa`. The Ifá contract/fixture-pack
  system is a consumer of replayed facts, not a dependency of the replay
  plumbing.
- No network calls, no credentials, no graph backend. Wrapping does not change
  what the delegate does — it only changes who is allowed to call it and when.

## Verifying a change

```bash
export GOCACHE="$(git rev-parse --show-toplevel)/.gocache"
cd go && go test -race ./internal/replay/concurrentreplay/... -count=1
```

The concurrency proof (`TestSourceConcurrentNextDeliversEachGenerationExactlyOnce`)
must run under `-race` and must fail without the mutex — that is the point of
this package.

## No-Regression Evidence

This package is additive replay infrastructure. It wraps `collector.Source`;
it does not modify `cassette.Source`, `collector.Service`, or any other
existing package. The only shared mutable state introduced is the wrapper's
own `mu`/`done`/`served` fields, guarded end-to-end by one mutex held across
each `Next` call. Verified by `go test -race ./internal/replay/concurrentreplay/... -count=1`.

## No-Observability-Change

No telemetry instruments, spans, logs, or status fields are added or modified
by this package. `Source` returns `collector.CollectedGeneration` values
unchanged from the delegate; the collector's existing telemetry (wired through
`collector.Service`) records normally once this wrapper is used as a driver's
source in a later slice.
