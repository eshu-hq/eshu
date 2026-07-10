# replay/faultreplay

The Layer 4 (deterministic fault injection) **fault script and hermetic
runner** for the Ifá conformance platform (design doc
`docs/internal/design/4389-ifa-conformance-platform.md`, #4580).

This package has two halves:

- **S1 — the fault script** (`script.go`): versioned, fail-closed **data**
  describing what to break and when. No reducer wiring, no decorator, no
  backend restart logic lives here.
- **S2 — the hermetic runner** (`source.go`, `executor.go`, `runner.go`):
  drives a validated `Script` through the **real** `reducer.Service` loop, no
  Docker/Postgres/graph backend, by decorating the two seams `service.go`
  already exposes (`WorkSource`/`BatchWorkSource` and `Executor`,
  `go/internal/reducer/service.go:27-56`).

`restart-backend-between-phase-groups` needs a real graph backend to restart,
so it cannot run hermetically; both `NewFaultingWorkSource` and
`NewFaultingExecutor` reject it at construction (a later S4 slice owns it),
rather than silently accepting a scripted fault that can never fire.

## What it is

`schedulereplay` (R-13, Layer 3) already scripts *delivery order* — in-order,
reverse, rotated, duplicate — as data driven through the real reducer loop.
`faultreplay` extends the same idea to *failure*: a `Script` names faults to
inject and the ordinal event (or stable ID) that fires each one, so a fault
run is scripted and replayable exactly like an ordering run.

```json
{
  "version": 1,
  "faults": [
    {"kind": "kill-worker-after-claim", "trigger": {"after_claims": 3}},
    {
      "kind": "fail-graph-write-once-then-succeed",
      "trigger": {"statement_ordinal": 1},
      "target": {"lane": "executor-retry"}
    }
  ]
}
```

## The five fault kinds

| Kind | Trigger | Target | Mechanism it exercises |
|---|---|---|---|
| `kill-worker-after-claim` | `after_claims` (>= 1) | — | lease-expiry reclaim (worker-lifecycle side) |
| `expire-lease-mid-handler` | `intent_ordinal` **xor** `intent_id` | — | lease-expiry reclaim (handler side) |
| `fail-graph-write-once-then-succeed` | `statement_ordinal` **xor** `operation_match` | `lane` (required) | retry-with-backoff + idempotent replay (`MERGE` / `ON CONFLICT`) |
| `restart-backend-between-phase-groups` | `after_phase_groups` (>= 1) | — | recovery across a backend outage |
| `fail-terminal` | `intent_id` | — | names the intent expected to become a durable dead-letter |

`fail-graph-write-once-then-succeed`'s `target.lane` is load-bearing, not
decorative: it must be `executor-retry` (a transient error retried in place by
the reducer's `RetryingExecutor`) or `queue-retry` (a transient error that
surfaces to `WorkSink.Fail` and is re-queued as a retrying intent). A script
that does not say which lane it expects cannot assert which recovery path
actually ran (proven in P6 T1). The hermetic runner and the in-binary
`cypher.FaultingExecutor` realize the lanes differently: the hermetic runner
drives the re-queue with `RedeliverOnce` (its `queue-retry` error may be a plain
non-`RetryableError`), while the in-binary decorator relies on the real reducer
queue, so its `queue-retry` error is a retryable `graph_write_timeout` error
like a real transient. Both assert a fault-free-identical drain with zero dead
letters.

## The determinism invariant

Every trigger field is an **ordinal** over observed events or a **stable
string ID** — never a duration, wall-clock timestamp, or random draw. A
wall-clock trigger would fire at a different point on every run, making the
fault run non-replayable and defeating the byte-identical canonical-graph
assertion the wider Layer 4 gate makes. `Script.Validate` enforces this:

- `version` must equal `1`; any other value is a hard parse error.
- `kind` must be one of the five kinds above.
- `expire-lease-mid-handler` and `fail-graph-write-once-then-succeed` each
  require exactly one of their two trigger fields — not both, not neither.
- Every ordinal (`after_claims`, `intent_ordinal`, `statement_ordinal`,
  `after_phase_groups`) must be `>= 1`.
- `fail-graph-write-once-then-succeed` requires `target.lane` to be
  `executor-retry` or `queue-retry`.
- Any populated `after_duration`, `at_timestamp`, or `random_seed` trigger
  field is rejected outright — these fields exist in the schema only so
  `Validate` has something concrete to reject; no real fault kind ever sets
  them.
- A trigger field that is a real field name but belongs to a *different* fault
  kind (e.g. `kill-worker-after-claim` setting `statement_ordinal`, which
  belongs to `fail-graph-write-once-then-succeed`) is rejected. This is the one
  class `DisallowUnknownFields` cannot catch on its own, since the field name
  is real — just wrong for this kind.

`Parse` also rejects any JSON field the schema does not know about
(`json.Decoder.DisallowUnknownFields`), so a typo or a made-up field fails
loudly at parse time rather than being silently ignored. It also rejects any
trailing content after the script's single JSON value — `json.Decoder.Decode`
only reads the first value off a stream, so a second document (or any other
JSON) appended after a valid script would otherwise parse as if only the first
object existed.

## Codec

```go
script, err := faultreplay.Parse(jsonBytes) // decode + Validate
script, err := faultreplay.Load(path)       // read file + Parse
err := script.Validate()                    // re-check a Go-constructed Script
```

## The hermetic runner (S2)

`RunFault` mirrors `schedulereplay.RunScheduleReport` exactly: it drives the
same in-memory `schedulereplay.Graph`/`WorkItem`/`Applier` model through the
**real** `reducer.Service` claim/execute/ack loop, except the `WorkSource` and
`Executor` it wires in are `FaultingWorkSource` and `FaultingExecutor` instead
of the plain `schedulereplay` types.

```go
report, err := faultreplay.RunFault(ctx, faultreplay.Config{
	Items:   schedulereplay.ScheduleInOrder(items), // same fixture as schedulereplay
	Workers: 4,                                     // >= 2 required for expire-lease-mid-handler
	Apply:   schedulereplay.ApplyCanonical,
	Script:  faultreplay.Script{ /* ... */ },
})
// report.Snapshot        -- the converged canonical graph
// report.Acked           -- successful completions, including recovered redeliveries
// report.FailedIntentIDs -- the terminal/dead-letter set (only fail-terminal survives here)
```

Each fault kind is modeled as a decoration of one of the two seams
`reducer.Service` already exposes for this purpose
(`go/internal/reducer/service.go:27-56`) — never a hook inside the service
loop, a handler, or a collector:

- **`kill-worker-after-claim`** (`FaultingWorkSource`): on the Nth global
  claim, the claimed intent is pushed onto an internal redelivery queue and
  handed out again on a later `Claim`/`ClaimBatch` call. No goroutine is
  actually killed — Go has no safe way to do that — so the observable effect
  (a duplicate delivery) is what the fault exercises, exactly like
  `schedulereplay.ScheduleWithDuplicates` but triggered by an ordinal instead
  of being baked into the schedule up front.
- **`expire-lease-mid-handler`** (`FaultingWorkSource` +
  `FaultingExecutor`, coordinated through the small `redeliverer` interface):
  when `FaultingExecutor.Execute` begins the one call for the targeted intent,
  it calls `ArmMidHandlerDuplicate`, which enqueues a duplicate delivery and
  returns a channel; the original call blocks on that channel (with a
  `ctx.Done()` escape, never an unconditional wait) until some **other**
  worker's `Claim` pops the duplicate and closes the channel. Because the
  parked goroutine cannot itself call `Claim`, the duplicate is *provably*
  claimed by a second, concurrently-running worker — this requires
  `Config.Workers >= 2`; `RunFault` refuses a `Workers < 2` config with that
  fault rather than deadlock. The eventual double-apply is real (T4): only its
  *order* is serialized, by the same graph mutex `schedulereplay`'s executor
  uses.
- **`fail-graph-write-once-then-succeed`** (`FaultingExecutor`): on the
  matching Execute call (by ordinal or `IntentID` substring — this hermetic
  tier has no separate per-statement Cypher boundary, so `statement_ordinal`
  counts `Execute` calls and `operation_match` matches the `IntentID`), fires
  exactly once per `target.lane`:
  - `executor-retry`: the injected failure is simulated and absorbed entirely
    inside `Execute` — it never reaches the caller — so `reducer.Service`
    observes exactly one successful call, mirroring
    `internal/storage/cypher.RetryingExecutor`'s retry-inside-`Execute`
    precedent.
  - `queue-retry`: `Execute` calls `RedeliverOnce` (enqueuing a
    fire-and-forget redelivery) and then returns a plain, non-`RetryableError`
    error, which `reducer.Service` routes to `WorkSink.Fail` — the redelivered
    attempt then succeeds normally.
- **`fail-terminal`** (`FaultingExecutor`): the targeted intent fails, every
  time it is delivered, without ever calling the inner executor. Nothing
  re-arms a redelivery for it, so it is the one fault kind that survives in
  `Report.FailedIntentIDs` to the end of the run.

`Config.Workers < 2` is only rejected for `expire-lease-mid-handler`; the
other three faults run correctly (and are exercised in tests) with
`Workers = 1`, since none of them requires two workers to be genuinely
in-flight at once.

### Inert-script detection

A scripted fault whose trigger never matches anything real -- a
`statement_ordinal` or `after_claims` ordinal the run never reaches, an
`intent_id` that never appears in the schedule, an `operation_match` that
never matches -- is a broken or inert script, not a valid fault-free pass.
`RunFault` verifies, after the run drains and before it reports success, that
**every** scripted fault actually fired at least once; if any did not, it
returns an error naming the unfired fault kind and trigger instead of
snapshotting the (accidentally fault-free) graph. This closes the
"measured-inert false-green" gap the wider Ifá conformance platform exists to
catch: without it, a typo'd ordinal or a stale intent ID would let the Layer 4
acceptance pass on a script that asserted nothing.

The two decorators expose this per-fault firing state directly, so a test can
assert it without running a full `RunFault`:

- `FaultingWorkSource.UnfiredFaults()` -- `kill-worker-after-claim`.
- `FaultingExecutor.UnfiredFaults()` -- `expire-lease-mid-handler`, both lanes
  of `fail-graph-write-once-then-succeed`, and `fail-terminal`.

(`FaultingWorkSource.InjectedRedeliveries()` and
`FaultingExecutor.ExecutorRetryFired()` remain for the narrower
"did this one redelivery/lane happen" checks predating this generic
per-fault gate.)

## Verifying a change

```bash
export GOCACHE="$(git rev-parse --show-toplevel)/.gocache"
cd go && go test ./internal/replay/faultreplay/ -race -count=1
cd go && go vet ./internal/replay/faultreplay
```

No Docker, no Postgres, no graph backend — the hermetic runner drives the real
`reducer.Service` loop entirely in memory, so the whole package (schema and
runner) runs in the default `go test` pass. `-race` is load-bearing for the
runner: `expire-lease-mid-handler` deliberately puts two goroutines
concurrently in-flight on the same intent, so the package's teeth depend on
that path being race-clean, not merely green.

## Performance & observability evidence

- No-Regression Evidence: the hermetic runner and schema are a net-new package
  imported only by tests. The in-binary fault decorator
  (`go/internal/storage/cypher/fault_executor.go`) and its reducer wiring
  (`go/cmd/reducer/ifa_fault_wiring.go`, `main.go`) are gated behind the
  `ifafaultinjection` build tag with no-op `_off.go` defaults, so the default
  `eshu-reducer` binary is byte-free of them — `go tool nm` on the untagged
  binary shows zero fault symbols. There is therefore no production reducer
  throughput, queue-depth, or row-count regression to measure. Conflict domain
  and worker settings mirror `schedulereplay`: one in-memory canonical graph
  mutated under one mutex, `Workers=1` for the sequential-safe faults and
  `Workers>=2` for `expire-lease-mid-handler`. `go test -race` for the whole
  package completes in ~2s.
- No-Observability-Change: no telemetry instruments, spans, logs, or
  status fields are added or modified. The runner asserts the canonical
  graph-truth snapshot and the sink's acked/failed accounting directly, not a
  runtime metric, and the reducer's existing claim/queue instrumentation is
  untouched.
