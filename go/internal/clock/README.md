# internal/clock

Injectable time source for the reducer queue / lease / reap path.

## Why this exists

Lease expiry, claim visibility, and retry backoff are wall-clock decisions. Read
straight from `time.Now()` they are nondeterministic and cannot be replayed. The
deterministic replay framework (epic #4102) needs to advance time on demand to
drive lease expiry without sleeping. This package is the Layer 3 enabler (R-12,
issue #4121): one named `Clock` seam that production fills with the real clock
and replay/tests fill with a controllable one.

## Exported surface

- `Clock` — one method, `Now() time.Time`. The injected time source.
- `System() Clock` — the real wall clock. `System().Now()` is exactly
  `time.Now()`, so wiring it where a nil seam already fell back to `time.Now()`
  is behavior-preserving.
- `NowFunc(func() time.Time) Clock` — adapts a closure to a `Clock`
  (mirrors `http.HandlerFunc`). Lets the codebase-wide `Now func() time.Time`
  struct seam accept a `Clock` (`s.Now = clk.Now`) without changing that field
  convention.
- `Simulated` — a controllable `Clock` for deterministic tests and replay:
  - `NewSimulated(start time.Time) *Simulated`
  - `Now() time.Time`
  - `Advance(d time.Duration)` — moves time forward; `0` is a no-op; negative
    panics.
  - `Set(t time.Time)` — jumps to `t`; an earlier `t` panics.

## Invariants

- **Behavior-preserving default.** `System()` is `time.Now()`. The seam exists so
  replay can swap the clock; it does not change production timing.
- **Monotonic.** `Simulated` never moves backward (`Advance(<0)` / `Set(earlier)`
  panic). Lease and retry deadlines assume a monotonic clock; a backward jump
  would mask an expired lease as still held.
- **Concurrency-safe.** One `Simulated` may back many components; a single
  `Advance`/`Set` moves the time all of them observe. All methods take the
  internal mutex.
- **UTC at the use site.** `Now()` returns the raw time; callers apply `.UTC()`
  when persisting or comparing, matching the existing `now()` helpers.

## Who injects it

The reducer composition root (`go/cmd/reducer`) constructs one `clock.System()`
and threads it into the reducer queue, the queue observer, and the graph
projection phase repairer + repair queue. See those packages' `now()` helpers
for the consuming seam.
