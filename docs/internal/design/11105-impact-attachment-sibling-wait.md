# 11105: supply_chain_impact waits for its own SBOM attachment

**A supply_chain_impact pass no longer loads evidence while the same scope
generation's sbom_attestation_attachment work is in flight. The attachment's
terminal Ack reopens the pass at once.**

Issue: #11105 (part of #10860, refs #11087). The binding arbiter ruling is
`.claude/arbiter-11105.md`. Paths below are under `go/internal/` and were
verified at origin/main `8b6af10392`.

## The ordering, as traced

The two intents of one scope generation are not ordered. The issue's first
acceptance criterion asks for this trace.

- The projector builds both intents in one call:
  `projector/runtime/scope_generation_intents.go:169` (attachment) and `:178`
  (impact). They share one enqueue `now`
  (`storage/postgres/reducer_queue.go:316`) and the default conflict key
  (`reducer_queue_conflict.go:224-225`).
- The claim fence blocks only a claimed or running sibling
  (`reducer_queue_claim_query.go:104-112`). A pending or retrying attachment
  with a future `visible_at` (`:40`) leaves the impact row claimable.
- No readiness row names either domain (`reducer_queue_readiness_sql.go:275-298`).
  The completion catalog's producers for impact are container_image_identity
  and ci_cd_run_correlation only (`reducer/crossscope/dependencies.go:36-41`).
- Which row is claimed first rests on the work_item_id tiebreak. No code
  states that order.

So the gap (the ruling's case ii) is real and predates #11087. It happens when
the attachment retries with a future `visible_at`, the impact pass runs first,
and nothing re-runs the pass later.

## The fix

The fix is a same-scope-generation sibling wait, following the #8827 template.

- `reducer/supplychain/core/attachment_wait.go`: `beforeEvidenceLoad` runs the
  #9874 manifest-marker branch, then `awaitOwnSBOMAttachment`, before the
  fencing token and every evidence load. That includes the #5709 floor
  sample. It runs on every attempt.
- The probe is `ImpactStore.SBOMAttestationAttachmentState`. It runs
  `ImpactProducerStateQuery` with the domain bound to
  `sbom_attestation_attachment`. The SQL is not copied.
- The decision table:

  | Attachment state | Pass | Outcome |
  | --- | --- | --- |
  | no rows | proceeds | none |
  | in flight, anchor zero or under 30 minutes old | defers, writes nothing | `deferred_in_flight` |
  | in flight, anchor 30 minutes or older | proceeds | `bound_exceeded`, WARN |
  | none in flight, any dead_letter or failed | proceeds | `producer_failed`, WARN |
  | none in flight, all succeeded or superseded | proceeds | none |
  | probe error | counting failure (`factload.ClassifyFactLoadError`) | none |

- The deferral class is `supply_chain_impact_sbom_attachment_not_ready`. It
  is in `nonCountingReducerRetryFailureClasses`. Its `WaitingDeferralMap` row
  is `producer_domain_terminal`, `in_tx`, narrowed to
  `sbom_attestation_attachment`. The existing `wakeProducerTerminalAfterAck`
  wakes it, so no wake code was added.
- The bound is `crossscope.ProducerReadinessMaxWait` (30 minutes), measured
  from the attachment rows' `NewestInFlightWorkableAt`. It never compares
  attempt counts.
- Wiring: `cmd/reducer/wiring_handlers.go` passes
  `SupplyChainImpactAttachmentReadiness: producerreadiness.ImpactStore{DB: database}`,
  and `reducer/defaults_additive_domains_supply_chain.go` hands it to the
  handler. The cmd wiring test fails when the probe does not run.

Deadlock: the attachment handler waits on nothing, so there is no cycle.
security_alert_reconciliation waits on impact (#8827) and impact waits on the
attachment, which is a chain, not a loop. Starvation is bounded by the
10-minute backstop. The park re-check counts dead_letter as satisfied. The
lease fence, claim SQL, ack SQL, and conflict key are unchanged.

## Cross-scope variant (ruling item 3): characterized GREEN

`TestReducerContentionGateSupplyChainImpactCrossScopeAttachmentReachesItsOwnPassLive`
sets up three scopes:

- S1 holds the referrer and the image identity, and runs the attachment.
- S2 holds the SBOM document's component.
- A third scope holds the vulnerability record.

S2's impact pass runs before S1's attachment commits. S1's own impact pass,
run after its attachment, reads the S2 component and the vulnerability
cross-scope and publishes the attachment-backed finding. The test passed
before and after the fix, so no `reopenSupplyChainImpactHolderQuery` fan-out
was built.

## Residue

An attachment that commits after a pass recorded `bound_exceeded` or
`producer_failed` does not re-run impact until the next generation. That
matches the #8827 give-up precedent. After an operator replays a dead-lettered
attachment item, they should also replay that scope's `supply_chain_impact`
item. The telemetry page says so. Non-zero `bound_exceeded` or
`producer_failed` counts in QA reopen the question of a targeted
same-generation reopen on the attachment Ack.

The issue suggested making sbom_attestation_attachment a catalog producer or
covering it with the readiness floor. The arbiter rejected both, and the
sibling wait replaces them. The fan-out cost the issue asked for is below.

Pre-existing, reported, not changed here: the producer-terminal wake runs its
cross-scope leg for every narrowed producer domain. That leg reopens
scope-bound waits on the domain in every scope, so an attachment Ack in scope
B also reopens scope A's parked pass. The pass re-defers without spending an
attempt.
`TestReducerContentionGateSupplyChainImpactAttachmentForeignAckIsAHintLive`
pins this. The same leg already applies to the #8827 and #7816 waits.

## Evidence

Performance Evidence: the probe is one statement per findings pass. The
EXPLAIN (ANALYZE, BUFFERS) setup is 2,000 scopes x 5 generations x 35
domains, which is 350,000 `fact_work_items` rows, with
`sbom_attestation_attachment` as one domain (PostgreSQL 18, shared host,
load average 13.7). The plan is an `Aggregate` over an `Index Scan` on
`fact_work_items_scope_generation_idx`. The index condition is `scope_id` and
`generation_id`, and the filter is `stage` and `domain`. Execution took
0.034-0.037 ms and planning 0.164-0.175 ms, with 4 shared hit buffers and 1
read buffer. That is one run each, which bounds the cost but does not
benchmark it.
`TestReducerContentionGateSBOMAttachmentProducerStatePlanUsesScopeGenerationIndex`
fails on a sequential scan or a missing index.

The attachment Ack cost is 2 post-commit wake statements per acked attachment
generation, and there were 0 before. The two statements are the narrowed
`reopenWaitingRowsQuery` and the cross-scope leg, which is deduped per domain
in a batch, so a batch of two costs 3.
`TestSBOMAttestationAttachmentAckWakeStatementCount` pins these counts. Both
statements read only the parked set in `reducer_waiting_items`.

No-Regression Evidence: T2
(`TestReducerContentionGateSupplyChainImpactWithoutAttachmentWorkProceedsLive`)
shows a generation with no attachment work proceeds on its first claim with
`attempt_count` 1. The unit decision table shows that no rows, all succeeded,
and superseded-only states proceed silently, with no metric and no log. A nil
port keeps the pre-#11105 pass. The worker count, batch size, lease, and
retry settings are unchanged.

Before and after, on the real queue:

- T1 (`TestReducerContentionGateSupplyChainImpactWaitsForItsOwnAttachmentLive`)
  seeds an attachment row retrying with `visible_at` 2 minutes ahead.
- Before the fix, the impact pass ran: `first impact pass: error = <nil>`,
  attached findings 0.
- After the fix, the pass defers at attempt 1 with no write. A claim 3
  minutes later is fenced (parked). The attachment Ack reopens the pass
  before the 10-minute backstop. It publishes 1 attached finding and ends
  succeeded at attempt 1.

Concurrency proof
(`TestReducerContentionGateSupplyChainImpactAttachmentWaitUnderContentionLive`):

- Setup: 6 workers on a pooled database (24 connections), with lease 1 minute,
  retry delay 20 ms, and MaxAttempts 3. They drain 8 image scopes, each with a
  retrying attachment (`visible_at` 300-1000 ms ahead) and a pending impact
  row, plus 8 unrelated impact-only scopes. The queue runs on the real clock.
- Before: 24 rows (8 attachment and 16 impact).
- After: all 24 succeeded, with every impact row at attempt 1 and 8 attached
  findings. The conflict domain is (scope, scope id).
- Three runs drained in 0.90 s, 1.03 s, and 1.07 s, with 36 non-counting
  deferrals each time. Eight is the minimum, and the rest are cross-scope
  hint wakes.
- No statement errors and no 23505 occurred. At no claim did a scope hold two
  claimed or running rows.
- A lost wake would have left a row to the 10-minute backstop, outside the
  120-second hang guard.

The retry matrix
(`supply_chain_impact_attachment_wait_matrix_live_test.go`) covers:

- A dead-lettered attachment lets the pass proceed after the backstop.
- A pass that parks after its sibling is terminal is reopened in the same Fail.
- A duplicate attachment Ack is rejected before any wake.
- Five deferrals, past MaxAttempts, keep `attempt_count` at 1, and a
  torn-connection probe error then dead-letters at attempt 3.
- A superseded sibling, and another generation's in-flight attachment, do
  not hold the pass.
- A sibling silent for 31 minutes no longer holds the pass.

With the #11087 own-digest bound (rebased onto 63d79dfe9e):
`TestSupplyChainImpactAttachmentWaitThenOwnDigestBoundYieldsLateFindings`
replays fixture c of `criteria_expansion_image_arrival_test.go` through
`Handle`. The image scope's pass defers while its own attachment is retrying,
and writes nothing. After the attachment commits, the bounded pass publishes
the 4 own-digest findings that the late attachment adds to the image scope's
run. With the port unwired, the first pass runs instead of deferring and the
test fails.

Observability Evidence:

- `eshu_dp_supply_chain_impact_attachment_wait_total{domain,outcome}`, with
  outcomes `deferred_in_flight`, `producer_failed`, and `bound_exceeded`.
- The INFO defer line and the WARN proceed-without lines. They carry the
  scope, the generation, the attachment sibling counts, `producer_elapsed`,
  and `producer_max_wait`.
- `eshu_dp_reducer_executions_total{domain="supply_chain_impact",status="deferred"}`
  with the WARN `reducer execution deferred` line.
- `eshu_dp_reducer_waiting_wake_total` for the Ack wake.
- The operator page is
  `docs/public/reference/telemetry/supply-chain-impact-attachment-wait.md`.

## Not checked

- The deployed fleet rate of `deferred_in_flight` and of cross-scope hint
  wakes has not been measured. A QA reading after deploy should compare the
  attachment-wait counter with the `supply_chain_impact` execution rate.
- The full storage/postgres suite under `-race` was not run here. That is the
  coordinator's pre-push.
