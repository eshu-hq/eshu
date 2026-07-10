# replay/faultreplay — agent scope

## Owned surface

- `go/internal/replay/faultreplay/` — the Layer 4 fault-injection **schema**
  (S1, `script.go`) and **hermetic runner** (S2, `source.go`, `executor.go`,
  `runner.go`) for the Ifá conformance platform (#4580).

## Non-negotiable invariants

### S1 — schema (`script.go`)

- Every `Trigger` field MUST be an ordinal over observed events or a stable
  string ID — never a duration, wall-clock timestamp, or random draw.
  `Script.Validate` MUST keep rejecting the `after_duration`, `at_timestamp`,
  and `random_seed` fields; do not repurpose them into real trigger fields,
  and do not add a new time/duration/random field without also teaching
  `Validate` to reject it. A wall-clock trigger makes a fault run
  non-replayable, defeating the byte-identical canonical-graph assertion the
  wider Layer 4 gate exists to make.
- `version` MUST equal `1`. Any other value is a hard parse error — do not add
  a shim or best-effort decode for a different version in this package; a new
  version gets a new constant and an explicit migration decision, not silent
  coercion.
- `FaultOp.Target.Lane` on `fail-graph-write-once-then-succeed` MUST stay
  required and MUST only accept `executor-retry` or `queue-retry`. This field
  is load-bearing (proven in P6 T1): it is how a fault run asserts which
  recovery path — the reducer's `RetryingExecutor` in-place retry vs. a
  `WorkSink.Fail` queue-retry — actually observed the injected failure. Do not
  make it optional or free-form.
- `Parse` MUST keep `json.Decoder.DisallowUnknownFields()` so an unrecognized
  or misspelled script field fails loudly instead of being silently dropped.
  `Parse` MUST also keep rejecting trailing content after the script's single
  JSON value (`json.Decoder.Decode` only reads the first value off a stream) —
  do not let a second appended document silently parse as if only the first
  existed.
- `Validate` MUST reject a trigger field that is a real field name belonging
  to a DIFFERENT fault kind (`allowedTriggerFields` in `script.go`), not just
  an unrecognized JSON key. `DisallowUnknownFields` only catches a field name
  the schema has never heard of; it cannot catch `kill-worker-after-claim`
  setting `statement_ordinal`. Keep `allowedTriggerFields` in lockstep with any
  new `Kind*`/`Trigger` field pairing.

### S2 — hermetic runner (`source.go`, `executor.go`, `runner.go`)

- Decorate ONLY the two seams `reducer.Service` exposes for this purpose
  (`WorkSource`/`BatchWorkSource` and `Executor`,
  `go/internal/reducer/service.go:27-56`). Do NOT add a hook inside
  `runPerItemConcurrent`/`runBatchConcurrent`, a handler, or a collector — that
  is the anti-rewrite placement rule the design doc names explicitly (Layer 4,
  point 2).
- No real Docker, Postgres, or graph backend. This package must keep running
  in the default credential-free `go test` pass. `restart-backend-between-
  phase-groups` needs a real backend to restart; `NewFaultingWorkSource` and
  `NewFaultingExecutor` MUST keep rejecting it at construction (a later S4
  slice owns it) rather than silently accepting a scripted fault that can
  never fire.
- No wall-clock coordination anywhere in the runner (mirrors the schema
  invariant above at the mechanism level): the mid-handler rendezvous, the
  redelivery queue, and every fire-once gate MUST be driven by channels,
  atomics, or ordinals from the script — never a `time.Sleep`/timer used as a
  synchronization primitive. A sleep-based rendezvous would be flaky under
  load and is exactly the kind of thing `-race` and repeated runs exist to
  catch; if a change needs a timer for the mid-handler wait, that is a design
  regression, not a valid fix.
- `expire-lease-mid-handler`'s blocking wait in `FaultingExecutor.Execute`
  MUST stay `select`-guarded against `ctx.Done()`. It MUST NEVER become an
  unconditional channel receive: with `Config.Workers < 2` the parked goroutine
  is the only one that could ever claim its own duplicate, so an unconditional
  wait deadlocks the run forever. `Config.validate` MUST keep rejecting
  `Workers < 2` for this fault kind; do not relax that gate without also
  proving (not asserting) a Workers=1 path cannot deadlock.
- `extraDrainCount` MUST stay in lockstep with every fault kind that causes an
  extra completion event (kill-worker-after-claim, expire-lease-mid-handler,
  and the queue-retry lane of fail-graph-write-once-then-succeed each add
  exactly one). Adding a fault kind that redelivers without updating
  `extraDrainCount` makes `awaitFaultDrain` under-count and can report a
  partial run as fully drained.
- `faultingSink`'s reconciliation (an intent recorded on `Fail` is removed from
  `terminalFailed` if a later `Ack` lands for the same intent ID) MUST stay:
  `Report.FailedIntentIDs` is the DURABLE/terminal SET, not a raw Fail-event
  log. Only `fail-terminal` may legitimately survive to the end of a run;
  regressing this would make the queue-retry lane's transient (recovering)
  failure look identical to a real dead letter. `faultingSink.Fail` MUST also
  keep deduping: a repeated Fail for an intent ID already in `terminalFailed`
  (a duplicate schedule, or a redelivery fault targeting an
  already-fail-terminal-targeted intent) MUST NOT add a second copy of the
  same ID — `FailedIntentIDs` is a set, not an event log.
- Keep proving the fault fired, not merely that the run stayed green:
  `FaultingWorkSource.InjectedRedeliveries()` and
  `FaultingExecutor.ExecutorRetryFired()` exist so a test can assert a
  scripted fault actually took its intended path instead of silently
  no-op'ing. Do not remove these without an equivalent teeth-proof.
- `RunFault` MUST verify, after the run drains and before it reports success,
  that EVERY scripted fault fired at least once
  (`FaultingWorkSource.UnfiredFaults()` / `FaultingExecutor.UnfiredFaults()`,
  aggregated by `verifyAllFaultsFired`). A fault whose trigger never matches
  anything real (a bad ordinal, a stale intent ID, a non-matching
  `operation_match`) is an inert script, not a valid fault-free pass; without
  this check the run would converge on the fault-free graph and the Layer 4
  acceptance would pass vacuously. Adding a new fault kind or a new trigger
  field MUST come with an equivalent addition to whichever decorator's
  `UnfiredFaults` — do not let a new fault silently bypass this gate.

## Skill routing

- `golang-engineering` for Go edits and tests.
- `concurrency-deadlock-rigor` for any change to `source.go`, `executor.go`, or
  `runner.go` — the mid-handler rendezvous, the redelivery queue, and the
  concurrent reducer worker pool are exactly the shared-state/lock-ordering
  surface that skill covers.

## Verifying a change

```bash
export GOCACHE="$(git rev-parse --show-toplevel)/.gocache"
cd go && go test ./internal/replay/faultreplay/ -race -count=1
cd go && go vet ./internal/replay/faultreplay
```
