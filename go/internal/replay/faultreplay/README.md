# replay/faultreplay

The Layer 4 (deterministic fault injection) **fault script** for the Ifá
conformance platform (design doc
`docs/internal/design/4389-ifa-conformance-platform.md`, #4580).

This slice (S1) owns the fault-script schema only — versioned, fail-closed
**data**. It does not run anything: no reducer wiring, no decorator, no
backend restart logic. Those land in later slices (S2 runner, S4 decorators)
that consume a `Script` this package has already validated.

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

## Verifying a change

```bash
export GOCACHE="$(git rev-parse --show-toplevel)/.gocache"
cd go && go test ./internal/replay/faultreplay/ -count=1
cd go && go vet ./internal/replay/faultreplay
```

No Docker, no Postgres, no graph backend, no reducer wiring — this slice is
pure schema and runs in the default `go test` pass.

## Performance & observability evidence

- **No-Regression Evidence:** net-new package; it is not imported by any
  production runtime path yet (no edit under `go/internal/reducer`,
  `go/internal/storage`, `go/internal/queue`, or the service binaries), so
  there is no reducer throughput, queue-depth, or row-count regression to
  measure.
- **No-Observability-Change:** no telemetry instruments, spans, logs, or
  status fields are added or modified. This slice is schema validation only.
