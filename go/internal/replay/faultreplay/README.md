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
decorative: it must be `executor-retry` (a transient-classified error retried
in place by the reducer's `RetryingExecutor`) or `queue-retry` (a plain error
that surfaces to `WorkSink.Fail` and is retried through the queue). A script
that does not say which lane it expects cannot assert which recovery path
actually ran (proven in P6 T1).

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

`Parse` also rejects any JSON field the schema does not know about
(`json.Decoder.DisallowUnknownFields`), so a typo or a made-up field fails
loudly at parse time rather than being silently ignored.

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
