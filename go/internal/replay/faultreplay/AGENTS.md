# replay/faultreplay — agent scope

## Owned surface

- `go/internal/replay/faultreplay/` — the Layer 4 fault-injection **schema**
  slice (S1) for the Ifá conformance platform (#4580).

## Non-negotiable invariants

- This slice is **schema + codec + validation only**. Do not add a runner,
  decorator wiring, or reducer/backend integration here — those are separate
  slices (S2 runner, S4 decorators) that import a validated `Script` from this
  package. Adding runtime behavior here breaks the intended package boundary.
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
- Keep the package dependency-light: no import of collector/parser internals,
  and no import of reducer/cypher packages in this slice. If a later slice
  needs those, it belongs in the runner package (S2), not here.

## Skill routing

- `golang-engineering` for Go edits and tests.
- `concurrency-deadlock-rigor` when a later slice adds the runner that drives
  this schema through the reducer loop (not applicable to S1's pure-data
  surface).

## Verifying a change

```bash
export GOCACHE="$(git rev-parse --show-toplevel)/.gocache"
cd go && go test ./internal/replay/faultreplay/ -count=1
cd go && go vet ./internal/replay/faultreplay
```
