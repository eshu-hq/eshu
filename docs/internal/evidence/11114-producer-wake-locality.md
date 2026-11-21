# #11114 producer-terminal wake locality

Refs #11114. Arbiter ruling (binding):
[11114-wake-locality-ruling-2026-11-21.md](../arbiter/11114-wake-locality-ruling-2026-11-21.md).
Built on #11105 (merged as #11123).

## What changed

`wakeProducerTerminalAfterAck` ran the cross-scope leg
(`wakeProducerTerminalCrossScopeQuery`) for every narrowed producer domain.
That statement matched every scope-bound `producer_domain_terminal` wait
naming the domain, in any scope and generation. So one producer Ack reopened
every parked waiter of that domain fleet-wide:

- a `supply_chain_impact` Ack reopened every parked #8827 reconciliation
  (`security_alert_reconciliation_impact_not_ready`);
- an `sbom_attestation_attachment` Ack reopened every parked #11105 impact
  pass (`supply_chain_impact_sbom_attachment_not_ready`).

Each reopened waiter cost a claim, a probe, a non-counting Fail, and a
re-park. With N parked waiters and N acks the total is N + N(N-1)/2
deferrals. Each such Ack also took `FOR UPDATE` on every parked wait row of
the domain and updated every matching queue row in one statement.

The leg has one legitimate consumer: `service_catalog_correlation_runtime_not_ready`
(#7816). Its fence reads the `workload_materialization` rows of the
correlated repository scopes, while the wait is keyed to the catalog scope,
so the keyed wake can never match it (#11053).

The fix:

- `WaitingDeferral.ProviderLocality` is a closed field with no valid zero
  value (`same_scope_generation` or `cross_scope`). Every narrowed
  producer-terminal row declares one: RuntimeNotReady is `cross_scope`;
  ImpactNotReady and SBOMAttachmentNotReady are `same_scope_generation`.
- The post-ack wake builds a plan once from the map. It issues the
  cross-scope statement only for a domain that has a cross-scope class, and
  passes those classes as `$3`. Both the `due` and `cleared` CTEs add
  `failure_class = ANY($3::text[])`.
- Unchanged: the reopen SET clause, the retrying-only reopen, the
  `work_item_id COLLATE "C"` lock order, and the keyed wake. A wait row's
  `failure_class` is set at park time and never rewritten (the insert upsert
  touches only `updated_at`).
- A narrowed row with an invalid locality is planned as cross-scope, so a
  missing declaration over-wakes instead of losing a wake. The completeness
  test still fails on it.

## No-lost-wake argument for same-scope classes

- The ack-side key is byte-identical to the park key:
  `{scope_id, generation_id, producer_domain}` of the waiter.
- Park after Ack: Fail runs autocommit statements in order (retry UPDATE,
  wait INSERT, re-check). Either the post-commit keyed wake sees the
  committed wait row, or the re-check sees the committed Ack.
- Duplicate Ack: the lease fence rejects it before any wake.
- Crash between the Ack commit and its wake: the audit re-checks the
  waiter's own scope and generation within one audit interval (5 minutes);
  the 10-minute backstop bounds it.
- Latency shift, not a lost wake: a waiter whose own producer row ends
  `superseded` or `dead_letter` without an Ack gets no keyed wake. It now
  reopens through the audit, within one 5-minute interval
  (`waitingSatisfiedQuery` treats `superseded` and `dead_letter` as
  terminal), where before a foreign Ack of the same domain often reopened it
  sooner. The wait stays bounded by the audit and the 10-minute backstop.

## Performance Evidence:

Backend: PostgreSQL 18.6 (`pg-11082`, Debian 18.6-1.pgdg13+2, aarch64),
disposable database with the full shipped bootstrap. Shared host under
load.

Contention proof
(`TestReducerContentionGateSupplyChainImpactAttachmentWaitUnderContentionLive`):

- Setup: 6 workers on one pooled database (24 connections), lease 1 minute,
  retry delay 20 ms, MaxAttempts 3 (unchanged). 8 image scopes, each with a
  retrying attachment row and a pending impact row, plus 8 unrelated
  impact-only scopes. Conflict domain: (scope, scope id).
- Before: at the #11105 head `4393e0faf9`, with that branch's version of the
  test, 2 runs took 36 and 30 non-counting deferrals for 8 scopes. The count
  varies there because its attachments became claimable on a fixed
  300-1,000 ms stagger, so the number of passes parked at each Ack depended
  on timing. (The pre-rebase stack head `c1bf27b318` took 36 in both of its
  runs.)
- After, with the attachment hold below: 7 runs (6 plain, 1 under `-race`
  together with the park-after-ack race test), exactly 8 each, one per image
  scope. No 23505, no 40P01, no statement error, 0 fence overlaps, every
  impact row succeeded at attempt 1. Drain time 837-947 ms.
- The test now holds the attachments unclaimable until every impact pass
  parked, then releases them 100 ms apart. Before the hold, 4 plain runs and
  5 `-race` runs (1 alone, 4 together with the race test) took exactly 8,
  but 1 more combined `-race` run failed before the final log line. Its
  cause was not captured; most likely a pass was claimed after its
  attachment already acked, so it took 0 deferrals. With the hold the fixed
  plan's count is deterministic, and the per-scope check names the scope
  that drifts.
- Mutation check: restoring the old plan (every narrowed class cross-scope)
  on this branch's test gives 22 to 36 deferrals across runs, depending on
  Ack order (29, 22, and 36 in the reviewer's three runs; 36 with per-scope
  counts 1, 2, ..., 8 in this lane's one run, beside the pre-fix samples 36,
  36, 36, and 30 above). 36 is the N(N+1)/2 worst case; the fixed plan is
  exactly 8.

`EXPLAIN (ANALYZE, BUFFERS)` of the cross-scope statement, with 6,000 parked
reconciliation waits (ImpactNotReady, one scope each) and 5 parked catalog
waits (RuntimeNotReady), after `ANALYZE`, each in a rolled-back
transaction. One run each: this bounds the cost, it is not a benchmark.
"Before" is the shipped statement with the two `failure_class` lines
removed; "after" is the statement as committed in `5986990e9e`.

| Ack | Before | After |
| --- | --- | --- |
| `supply_chain_impact` (per Ack) | 1 statement: Seq Scan on `reducer_waiting_items`, locks and reopens 6,000 rows, 182,002 shared hit, 531 dirtied, 157.7 ms | no cross-scope statement (keyed wake only) |
| `workload_materialization` (per Ack) | Seq Scan (6,000 rows removed by filter), 382 shared hit, 4.48 ms | Index Scan on `reducer_waiting_items_reason_idx` (`failure_class = ANY(...)`), 148 shared hit, 0.28 ms |

Statements per Ack (`TestSBOMAttestationAttachmentAckWakeStatementCount`):
an attachment or impact Ack drops from 2 to 1 (the keyed reopen), a batch of
two attachment Acks from 3 to 2, and a `workload_materialization` Ack keeps
its keyed wake plus one filtered cross-scope statement (deduped per batch).

No-Regression Evidence: the catalog lost-wake guard
(`TestProducerTerminalWakeCatalogRuntimeCrossScopeLive`) stays green: a
`workload_materialization` Ack in another repository scope still reopens the
parked catalog wait. The seeded variant (catalog class declared same-scope)
loses that wake, so the guard can see a regression. The matrix
(`reducer_waiting_wake_locality_matrix_live_test.go`) covers the park-after-ack
race on separate connections (8 scopes), duplicate Ack, Ack without wake then
one audit pass, a mixed AckBatch (cross-scope and same-scope domains), and a
superseded generation, each with no foreign Ack present. The #8827
reconciliation contention proof
(`TestReducerContentionGateSecurityAlertReconciliationImpactFenceUnderContention`,
12 scopes, 6 workers) takes 12 non-counting deferrals before and after (2 runs
each at `4393e0faf9` and at this branch): its interleaving does not expose the
fan-out, and the fix does not change it.

Residual, not changed: each `workload_materialization` Ack still reopens
every parked catalog wait (the catalog class is cross-scope by design).
Per-repository re-keying of the catalog wait is out of scope (arbiter: follow
up only if QA shows the residual matters).

## Observability Evidence:

No new instrument (arbiter ruling item 3). The QA before/after reading:

- `eshu_dp_reducer_retry_surge_total{failure_class}` divided by succeeded
  producer items, for `security_alert_reconciliation_impact_not_ready` and
  `supply_chain_impact_sbom_attachment_not_ready`: spurious re-deferrals
  show as a ratio above 1 per producer Ack, and should fall to about one
  deferral per waiter.
- `eshu_dp_reducer_waiting_items{failure_class}`: the parked count per class.
- `eshu_dp_reducer_waiting_wake_total{site,outcome}` counts wake statements
  only; after the fix it records one fewer `in_tx` statement per impact or
  attachment Ack.

## Not checked

- QA parked counts and the real spurious ratio: the QA "before" reading must
  be taken before this change deploys.
- Pre-existing, out of scope (arbiter note): the same-scope park re-check
  treats any one terminal producer row as satisfied while handlers wait for
  none in flight. With several producer rows per scope generation this can
  re-park in a loop.
