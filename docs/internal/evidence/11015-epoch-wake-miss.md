# #11015: wake parked generation_active waits on projector Fail and Claim supersession

Issue #11015: the #10876 parking change defers `generation_activation_not_ready`
reducer intents with a 10-minute backstop, but only the projector Ack
activation reopened those waits. A waiter parked before a terminal projector
Fail or before a Claim-path sweep that supersedes its generation sat out the
backstop. The three `TestContainerImageIdentityFirstGeneration*Live` proofs
advance about a minute, so their post-provider claims found no work (RED on
clean `origin/main`, all three fail).

## Change

`go/internal/storage/postgres/` only, no DDL, no new metric:

- `projector_queue_ack.go`: the Ack wake becomes `wakeGenerationActiveWaits`
  (same statement, same error text); Ack behavior unchanged.
- `projector_queue.go`: the terminal projector Fail path (dead-letter, the
  only path that retires the generation; the retry path keeps it pending and
  wakes nothing) calls the shared wake after the dead-letter commits.
- `projector_queue_scan.go` (`claimOnce`): after a successful claim, probe
  parked `generation_active` waits of the claimed scope whose generation is
  now terminal (`wakeTerminalGenerationActiveWaits`) and reopen them. The
  probe is one lock-free SELECT; the wake reuses the #10876 helper with the
  same lock order as the audit.
- `reducer_waiting.go`: the `generation_activation_not_ready` deferral entry
  names all three providing saves; the probe helper lives here.
- `reducer_waiting_store.go`: the `generation_active` satisfaction predicate
  resolves on terminal truth (`active`, `failed`, `superseded`), so a waiter
  that parks after its provider committed reopens at once and the audit
  fallback covers the rest.
- Tests: the epoch barrier advances past the backstop while parked; all three
  proofs freeze the projector clock on the test clock. Claim unit fakes
  answer the probe with zero rows; the source-system claim test pins the
  probe as the second query, and the Fail lifecycle test pins wake/no-wake
  on the dead-letter/retry paths.

## Lock and cost analysis (why this is safe)

- The per-claim probe takes no locks (plain SELECT) and runs only after the
  claim commits, so it cannot join the claim's lock order or deadlock it. A
  waiter that parks after the probe is covered by the set-aside immediate
  re-check, which shares the widened predicate.
- The post-commit Fail/Claim wakes use the same statement and lock order as
  the audit wake, so they cannot deadlock each other; the #10876 design
  already treats every wake as a hint the claim gate and handler re-check.
- Steady-state added work per projector claim: one indexed SELECT returning
  zero rows. Per terminal projector Fail (rare path): one wake statement,
  a no-op when nothing is parked.

## Measurements

Backend: disposable `postgres:18.6` on Alpine (the live-postgres-readiness
proof image), private schema per test via the standard proof bootstrap.

`EXPLAIN (ANALYZE, BUFFERS)` of the new probe statement, input shape one
parked `generation_active` wait on a superseded generation vs none parked:

- One parked terminal row: nested loop over
  `reducer_waiting_items_wake_idx` (`waits_for_kind = 'generation_active'`)
  and `scope_generations_scope_generation_idx` (`scope_id`), 1 row,
  Execution Time 0.025 ms (Planning 0.159 ms).
- No parked rows: same index probe, inner side never executed, 0 rows,
  Execution Time 0.023 ms (Planning 0.064 ms).

Terminal queue/row counts: the probe returns `DISTINCT waits_for_key`, so one
wake statement per parked key; the parked-waits table holds only deferred
intents (near-empty in steady state), and the wake UPDATE only touches rows
still `retrying`, deleting exactly the wait rows it reopened.

End to end (same disposable backend): the three issue proofs RED on main
(`claim ... = (false, <nil>)` in the parked window) and GREEN after
(`-run 'TestContainerImageIdentityFirstGeneration(EpochBarrier|FailureExits|SupersededWhileNull)Live'`,
9.3 s); all `TestContainerImageIdentityFirstGeneration*` including the
service-dispatch proof GREEN (14.0 s); the waiting predicate proofs
(`SetAsideParksWithBackstop`, `BothOrdersEquivalence`,
`AuditFindsMissedWake`, `ConcurrentClaimAndWake`) GREEN (7.4 s); focused
queue unit suites (Ack/Fail/Heartbeat/Claim/reducer-Fail/waiting-map)
GREEN (<0.4 s).

No-Regression Evidence: per projector claim the change adds one ~0.02 ms
indexed SELECT that returns zero rows in steady state, against a claim that
already runs a multi-CTE sweep plus a fence-row bump; per terminal projector
Fail it adds one existing wake statement that is a no-op when nothing is
parked. Plan shapes above show index-only access on both sides with no heap
growth, and the end-to-end proof wall times match pre-change runs.

Observability Evidence: no new metric, span, or log. The new wakes report on
the existing `eshu_dp_reducer_waiting_wake_total` counter with the existing
`in_tx` site value (no new label cardinality); a missed wake still surfaces
on `eshu_dp_reducer_waiting_missed_wake_total` via the audit, and the
terminal row shapes are asserted directly by the live proofs above.
