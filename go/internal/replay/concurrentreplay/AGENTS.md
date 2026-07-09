# replay/concurrentreplay — agent scope

## Owned surface

- `go/internal/replay/concurrentreplay/` — the thread-safe `collector.Source`
  wrapper (`Source`, in `source.go`) and the concurrent replay `Driver`
  (`Driver`, `Report`, in `driver.go`) for the Ifá P2 concurrent replay driver
  (issue #4395, parent epic #4389).

## Key invariants

- `Source.Next` MUST hold its mutex across the entire delegate call. The
  delegate (`cassette.Source` today) performs no I/O; the lock only serializes
  an in-memory cursor advance, never the commit stage. Do not narrow the lock
  scope to "just increment a counter" — the delegate itself is not safe for
  concurrent calls and needs the same protection the counter does.
- The one-shot `done` latch MUST be permanent: once the delegate reports
  `ok=false` or a non-nil error, every subsequent `Next` call — from any
  goroutine, for the rest of the `Source`'s lifetime — MUST return
  `ok=false, err=nil` without invoking the delegate again. This is what
  defeats `cassette.Source`'s poll-restart (it resets its scope cursor to 0
  and replays after the first `ok=false`). Do not "fix" this by re-checking
  the delegate after a drain; that reintroduces double-replay under
  concurrent callers.
- An error from the delegate MUST surface exactly once (on the call that
  observed it) and MUST also latch `done=true`. A caller that ignores the
  error and retries MUST see a permanently drained source, not a retried
  delegate call.
- `Source` MUST NOT import `internal/ifa`. It speaks only the top-level
  `collector.Source` / `collector.CollectedGeneration` contract so any
  single-threaded replay flavor can be wrapped, not just `cassette.Source`.
- `Driver.Run` MUST fail fast: the first error from `Source.Next` or
  `Committer.CommitScopeGeneration` MUST cancel the derived context so the
  other workers stop draining promptly, and MUST be the error `Run` returns.
  Use a `sync.Once` (or equivalent) so only the first error is latched and
  `cancel` is only called once.
- `Driver.Run` MUST NOT reduce `Workers` in response to errors or contention.
  Shrinking worker count is the repository's Serialization-Is-Not-A-Fix
  anti-pattern. `Workers <= 0` defaulting to `1` is a valid *sequential* run
  configuration, not a concurrency workaround — it is chosen up front by the
  caller, not adjusted at runtime in reaction to a failure.
- `Driver.Instruments` MUST NOT be used to mint a new metric name. It is
  reserved for a later slice to thread an existing `eshu_dp_*` instrument
  through; adding a new counter/histogram here without a design decision on
  which existing instrument applies is out of scope.
- Do not add the `FactSliceSource`, the `fact_work_items` fan-out, or the
  reducer drain harness to this package — those are later slices of #4395.
  This package owns the `Source` wrapper and the `Driver` that drains it, not
  the fan-out or DB harness around them.

## Skill routing

- `concurrency-deadlock-rigor` for any change to the lock scope, the drain
  latch, the `Driver` fail-fast path, or how the delegate/committer is
  invoked.
- `golang-engineering` for Go edits and tests.

## Do not

- Narrow the mutex to exclude the delegate call.
- Let the delegate's poll-restart reach a concurrent caller.
- Add network calls, credentials, or a graph-backend dependency to this
  package.
- Import `internal/ifa` or any specific replay flavor package (`cassette`,
  `schedulereplay`, ...) from `source.go` — the delegate is always the
  `collector.Source` interface, injected by the caller.
- Reduce `Driver.Workers` at runtime, or otherwise serialize `Driver.Run`, as
  a response to a commit error or observed contention.
- Add `golang.org/x/sync/errgroup` or any other new module dependency to this
  package without checking `go.mod`/`go.sum` first — as of this writing it is
  not a dependency, and `Driver.Run` uses a plain `sync.WaitGroup` plus a
  `sync.Once`-guarded first-error field instead.

## Verifying a change

```bash
export GOCACHE="$(git rev-parse --show-toplevel)/.gocache"
cd go && go test -race ./internal/replay/concurrentreplay/... -count=1
```
