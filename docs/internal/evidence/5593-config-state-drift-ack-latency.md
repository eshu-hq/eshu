# #5593 — `config_state_drift` runtime trigger: `Ack` latency under admission backpressure

## What changed and why this needs its own evidence

`ProjectorQueue.Ack` (`go/internal/storage/postgres/projector_queue.go`) now
calls `runConfigStateDriftTriggerHook` after every commit. For a
`state_snapshot:*` scope on the ingester's `ProjectorQueue`, that hook calls
`ConfigStateDriftRuntimeTrigger.TriggerConfigStateDrift`, which enqueues
through `reducerWriter` — the SAME admission-aware `projector.ReducerIntentWriter`
(`cmd/ingester/reducer_admission.go`'s `reducerAdmissionWriter`) the projector
runtime's own `IntentWriter` already uses. That writer's `Enqueue` is not a
fire-and-forget call: when the shared reducer queue is over its high-water
mark or under graph-write-timeout pressure, it blocks in
`for { ...; sleep(ctx, PollInterval) }` until admission clears. Before this
change, `Ack` never depended on reducer-admission state for ANY scope kind.
After this change, `Ack` for a `state_snapshot:*` scope is coupled to it.

`scripts/verify-performance-evidence.sh` passed on this branch, but for the
wrong reason: it greps the CURRENT full content of every touched
`README.md`/`AGENTS.md` for marker strings, not the diff, and
`go/cmd/ingester/README.md` and `go/internal/storage/postgres/AGENTS.md`
already carry dozens of markers from unrelated historical issues. This file
is the PR-specific record the gate's grep happened to accept without.

## Theory and measurement (Prove-The-Theory-First)

Performance Evidence: this section is the OLD-shape-vs-NEW-shape proof the
Prove-The-Theory-First gate requires, run on the same primitive
(`ProjectorQueue.Ack`) before/after the hook's added work.

Theory: `Ack`'s wall-clock return time for a `state_snapshot:*` scope is
`baseline + deferrals * PollInterval`, where `deferrals` is however many
admission-deferral cycles `reducerAdmissionWriter.Enqueue` runs before
succeeding, and `baseline` (the hook's own fixed cost — a scope-prefix check
plus one `Enqueue` call in the non-deferring case) is small and NOT coupled
to `PollInterval`.

Proof: `TestProjectorQueueAckConfigStateDriftTriggerLatencyUnderAdmissionBackpressure`
(`go/internal/storage/postgres/projector_queue_config_state_drift_trigger_hook_latency_test.go`)
measures `Ack` wall time on the real `ProjectorQueue.Ack` path (not a mock of
`Ack` itself) with a zero-cost fake DB, so the only variable is the hook's own
added work. `ConfigStateDriftTrigger` is a `sleepingConfigStateDriftTrigger`
that reproduces `reducerAdmissionWriter.wait`'s exact shape — `deferrals`
sequential `time.After(pollInterval)` waits before returning — at
`pollInterval = 20ms` (scaled down from a real deployment's typical
multi-second `PollInterval`; the relationship the test proves is linear in
`deferrals`, so the scaling factor does not change the conclusion, only the
wall-clock size of the demonstration).

Command and output (this branch, after the final edit to the test file):

```
cd go && go test ./internal/storage/postgres/... \
  -run 'TestProjectorQueueAckConfigStateDriftTriggerLatencyUnderAdmissionBackpressure' \
  -count=1 -v
```

```
=== RUN   TestProjectorQueueAckConfigStateDriftTriggerLatencyUnderAdmissionBackpressure
    projector_queue_config_state_drift_trigger_hook_latency_test.go:113: Ack latency over 20 iterations: baseline(no trigger)=112.333µs (5.616µs/op), no-deferral=7.25µs (362ns/op), 1-deferral=420.721083ms (21.036054ms/op), 3-deferrals=1.282520458s (64.126022ms/op), pollInterval=20ms
--- PASS: TestProjectorQueueAckConfigStateDriftTriggerLatencyUnderAdmissionBackpressure (1.70s)
PASS
ok  	github.com/eshu-hq/eshu/go/internal/storage/postgres	4.551s
```

Reading the four shapes against the theory:

| Shape | Measured per-op | Predicted (`deferrals * pollInterval`) |
| --- | --- | --- |
| baseline (`ConfigStateDriftTrigger` unwired — pre-#5593 behavior, and bootstrap-index's behavior today) | 5.6µs | n/a (hook never runs) |
| no-deferral (trigger wired, admission not deferring) | 362ns | 0 |
| 1-deferral | 21.0ms | 20ms |
| 3-deferrals | 64.1ms | 60ms |

The no-deferral case's added cost is sub-microsecond — noise-level, well
inside the ~5µs baseline itself. The 1- and 3-deferral cases land within a
few percent of the exact predicted `deferrals * pollInterval`, and the
3-deferral case is measurably larger than the 1-deferral case (linear, not a
one-time fixed cost). The theory is confirmed, not assumed.

## No-Regression Evidence

No-Regression Evidence: the regression this measurement rules out is silent,
unbounded `Ack` stalling for `state_snapshot:*` scopes. It is NOT unbounded:

- **Same admission policy every other reducer domain already lives under.**
  `reducerAdmissionWriter` already governs every intent the projector
  runtime's own `IntentWriter` enqueues for every scope kind, before this
  change — its `PollInterval`/`HighWaterMark`/graph-write-pressure knobs are
  an existing, already-tuned operational contract
  (`cmd/ingester/reducer_admission.go`, `loadReducerAdmissionConfig`). This
  change does not introduce a new backpressure mechanism; it extends the
  SAME one to one additional call site.
- **Bounded to one worker, not the whole ingester.** `projector.Service` runs
  `Workers` concurrent claim/project/Ack goroutines
  (`projectorWorkerCount(getenv)`, `cmd/ingester/wiring.go`). A blocked
  `state_snapshot:*` `Ack` ties up one worker's goroutine, not the service's
  collector loop or its other workers' claims.
- **Not on the hot path for the vast majority of `Ack` calls.** The hook
  exits immediately (`strings.HasPrefix` check, no query) for every scope
  that is not `state_snapshot:*` — the overwhelming majority of `Ack` calls
  in a normal fleet (git repos vastly outnumber Terraform-state scopes).
- **Admission deferring at all is itself an existing operator-visible
  condition.** `reducerAdmissionWriter.recordDeferral` already emits
  `eshu_dp_reducer_admission_deferrals_total` (by `reason`) and a structured
  warning log on every deferral cycle, regardless of which call site
  triggered it. An operator already has a signal that explains WHY a
  `state_snapshot:*` `Ack` is slow when this happens — it is the same signal
  that already explains a slow projector-runtime enqueue for any other
  scope.

No baseline `Ack` latency number exists for a `state_snapshot:*` scope before
this change because the hook (and therefore any admission coupling at all)
did not exist — this is a new dependency being added, not an existing
hot-path number regressing. The measurement above is that new dependency's
own before/after, on the same primitive, with a stated worst-case shape
(linear in deferral count) rather than an unbounded or unknown one.

## Observability Evidence

Observability Evidence: an operator diagnosing a slow ingester projector
worker for `state_snapshot:*` scopes watches:

- `eshu_dp_reducer_admission_deferrals_total` (existing, by `reason`) —
  confirms admission is actively deferring and why (`graph_write_pressure` vs
  `high_water_mark`), regardless of which call site is blocked on it.
- The existing `reducer admission deferring enqueue` structured warning log
  (`cmd/ingester/reducer_admission.go`'s `recordDeferral`), which carries
  `queue_depth`, `high_water_mark`, `retrying_high_water_mark`,
  `retrying_low_water_mark`, and `poll_interval` — enough to compute the
  worst-case wait directly.
- `eshu_dp_config_state_drift_runtime_trigger_failures_total` (new, issue
  #5593 P1-2) if the eventual `Enqueue` call errors outright rather than
  merely deferring.

No-Observability-Change: the coupling itself reuses `reducerAdmissionWriter`'s
existing deferral counter and log line verbatim -- no new metric, span, or
log field was needed to make the deferral state visible, because the SAME
admission writer already reports it for every other call site. (The one
genuinely new metric this branch adds,
`eshu_dp_config_state_drift_runtime_trigger_failures_total`, covers an
outright `Enqueue` error, not the deferral/backpressure case this evidence
file is about; see the P1-2 discussion in
`go/internal/storage/postgres/projector_queue_config_state_drift_trigger_hook.go`.)
