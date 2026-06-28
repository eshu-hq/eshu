# AGENTS.md — internal/clock guidance for LLM assistants

## Read first

1. `go/internal/clock/README.md` — purpose, exported surface, invariants
2. `go/internal/clock/clock.go` — `Clock`, `System`, `NowFunc`, `Simulated`
3. The consuming seams: `ReducerQueue.now()`
   (`go/internal/storage/postgres/reducer_queue_helpers.go`), the queue observer,
   and the graph projection phase repairer / repair queue

## What this package is for

This is the injectable time source for the reducer queue/lease/reap path
(issue #4121, R-12 of epic #4102). It does NOT own scheduling, sleeping, or
tickers — only "what time is it". Wait/poll loops stay on their existing
`Wait func(context.Context, time.Duration) error` seams.

## Invariants this package enforces

- **`System()` is behavior-preserving.** `System().Now()` is exactly
  `time.Now()`. Never make `System` do anything else; the whole point is that
  injecting it where a nil seam already fell back to `time.Now()` changes no
  behavior. Perf claims in #4121 depend on this.
- **`Simulated` is monotonic.** `Advance(<0)` and `Set(earlier)` panic. Do not
  "fix" these to clamp silently — a backward jump masks an expired lease as held
  and would make replay lie. A fail-loud panic is correct for a test/replay
  clock.
- **`Simulated` is concurrency-safe.** Keep every field access under `mu`. Replay
  shares one `Simulated` across many queue/lease components; a data race here
  corrupts every consumer at once.
- **`Now()` is not pre-UTC'd.** Call sites apply `.UTC()`. Keep it that way so
  this package matches the established `now()` helper convention and does not
  double-normalize.

## Common changes and how to scope them

- **Add a new consumer (another queue/lease store).** Give the store the standard
  `Now func() time.Time` field + `now()` helper (nil → `time.Now().UTC()`), then
  assign `clk.Now` at the composition root. Do NOT add a `clock.Clock` field type
  to storage structs — the codebase convention is the `Now func()` seam; stay
  consistent.
- **Need a sleep/tick that replay can control.** That is NOT this package. Use or
  extend the `Wait`/poll seam on the relevant runner; this package stays
  read-only "current time".
- **Add a method to `Clock`.** Resist it. A wider interface forces every adapter
  (`NowFunc`, `System`, `Simulated`) and every test fake to grow. `Now()` is
  deliberately the whole contract.
