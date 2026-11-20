# #11016: harden parked generation_active wakes (post-commit errors, residual transitions)

Issue #11016 (deferred F3/F4 from the #11015 review; #11015 itself merged as
#11019): a post-commit hint-wake error failed the already-committed
operation, and residual terminal transitions resolved only via the audit.

## Change

`go/internal/storage/postgres/` only, no DDL, no new metric or log key:

- F3: `Fail`, `claimOnce`, and the heartbeat supersede run their wakes
  best-effort after the commit. A failure is logged at ERROR and counted on
  the existing `eshu_dp_reducer_waiting_wake_total{outcome=error}` (the
  per-key wake already meters itself; the probe meters its own list/scan
  failures exactly once), never returned — so a probe 40P01 no longer reads
  as a claim conflict and the claimed work is never orphaned.
- F4: the terminal-wait probe is scope-free and runs on empty sweeps too,
  covering claim sweeps of other scopes; the heartbeat supersede reopens the
  superseded generation's waits; Ack probes for maintenance-superseded
  generations in its own transaction (pre-commit, so a failure still rolls
  back). The retirement fence, the generation-liveness sweep, and the
  delta-baseline refusal retire generations off the hot path with leases
  drained first, so they stay on the audit terminal-progress fallback (at
  most two 5-minute audit cycles) plus the 10-minute backstop — recorded in
  the `generation_activation_not_ready` deferral-map entry. Rebuild/reset
  only supersedes `relationship_generations` (a different wait kind with its
  own wake), so no `generation_active` disposition applies.

## Probe measurement

Disposable `postgres:18-alpine`, scratch tables with the shipped DDL and
indexes, two superseded generations with parked waits in two scopes plus an
active-generation wait and a noise kind:

- Scope-free probe returns exactly the two terminal keys across scopes
  (the active-generation wait and the noise kind excluded).
- `EXPLAIN (ANALYZE, BUFFERS)`: 0.064 ms with two parked waits, 0.015 ms
  with the parked table empty (tiny scratch tables seq-scan; in production
  the predicate leads with `waits_for_kind` on
  `reducer_waiting_items_wake_idx` and the parked table holds only deferred
  intents, the same basis as the #11015 0.02 ms indexed reading).

## Behavior proof

RED on the pre-fix tree (6/7 fail for the intended reason, 1 guard passes
before and after by design), GREEN after:

- `TestProjectorQueueFailPostCommitWakeErrorKeepsDeadLetter`
- `TestProjectorQueueClaimProbeErrorKeepsClaimedWork` (40P01 probe error:
  no retry, no conflict metric, error-outcome wake metric +1)
- `TestProjectorQueueClaimEmptySweepProbesTerminalWaits`
- `TestProjectorQueueClaimProbeCoversEveryScope`
- `TestProjectorQueueHeartbeatSupersedeWakesParkedWaits`
- `TestProjectorQueueHeartbeatSupersedeWakeErrorKeepsSuperseded`
- `TestProjectorQueueAckMaintenanceProbeRunsInTx`

Full `go/internal/storage/postgres` hermetic suite GREEN except the
pre-existing env-gated `TestSchemaBootstrapCoordinationDefaults...` (needs
`ESHU_DEPLOY_CHECKOUT`; fails identically on clean `origin/main`).

Observability evidence: no new signal. Wake failures log at ERROR with
`queue`/`op`/`scope_id?`/`generation_id?`/`error` and count on the existing
`eshu_dp_reducer_waiting_wake_total{wake_site=in_tx, outcome=error}`; a
missed wake still surfaces on
`eshu_dp_reducer_waiting_missed_wake_total` via the audit.

No-Regression Evidence: per projector claim the change adds one scope-free
indexed SELECT that returns zero rows in steady state (measured 0.015 ms
empty, 0.064 ms with two parked waits, disposable postgres:18-alpine
scratch tables with the shipped DDL and indexes; baseline is the #11015
scoped probe at 0.020-0.025 ms on the same shape). Per terminal projector
Fail it adds one wake statement that is a no-op when nothing is parked;
the heartbeat renew path adds nothing (the wake runs only when the
supersede statement returned a row); Ack adds one lock-free SELECT in its
transaction plus the existing wake shape when waits are parked. Terminal
queue/row counts: the probe returns DISTINCT parked keys (near-empty
table by design), and each wake UPDATE touches only still-`retrying` rows
it reopens. Safe because the probe takes no locks, the wakes reuse the
#10876 helper with the same lock order as the audit, and every wake is a
hint the claim gate and handler re-check, with the 5-minute audit and the
10-minute backstop bounding a miss.

Observability Evidence: no new metric, span, or log key. Wake failures
count on the existing
`eshu_dp_reducer_waiting_wake_total{wake_site=in_tx, outcome=error}` and
log at ERROR with `queue`, `op`, `scope_id`/`generation_id` when named,
and `error`; a missed wake counts on the existing
`eshu_dp_reducer_waiting_missed_wake_total` via the audit. Telemetry
emitters covered by `TestProjectorQueueFailPostCommitWakeErrorKeepsDeadLetter`
and `TestProjectorQueueClaimProbeErrorKeepsClaimedWork`, which assert the
error-outcome series is present.
