# #8956 reconciliation give-up state: evidence

A security-alert reconciliation that stops waiting for impact evidence writes
`unmatched` rows. Before this change those rows read the same as a genuine
no-match, so an API or MCP reader could not tell the two apart.

## Root cause

Root-Cause Evidence: `resolveImpactWait` reached its attempt cap with
`gave_up_unmatched` whenever no impact work was in flight, including when the
same generation's `supply_chain_impact` work had succeeded.
`TestSecurityAlertReconciliationGiveUpIsVisible` pinned that with the
fixture `{Total: 1}`. Every reconciliation intent is impact-producing (its
source system is `security_alert` or `package_registry`), so every `unmatched`
row passed through the give-up path. The give-up reason lived only on the
counter, the WARN log, the sub-signal, and the result summary, never on the
row.

## Decision

The arbiter ruled the narrowed Option B: a settled producer (at least one
impact row, none in flight, all `succeeded`) makes an `unmatched` row a
genuine no-match with no new key. Every other stop at the cap is a give-up and
persists the closed `impact_wait_outcome`. Skipping the counting retries once
the producer settles is a separate follow-up.

## Proof

| Case | Test | Result |
| --- | --- | --- |
| settled producer | `TestSecurityAlertReconciliationWriteCarriesTheGiveUpOutcome`, `...SettledProducerIsNotAGiveUp` | no outcome on the write, `producer_settled` at INFO, no WARN, no sub-signal |
| dead-lettered, mixed, superseded, never enqueued, fence unwired | same table | `gave_up_unmatched` |
| in flight past the bound | same table | `gave_up_bound_exceeded` |
| genuine no-match payload | `TestSecurityAlertReconciliationWriterKeepsTheGenuineNoMatchPayload` | sha256 equals the pre-change payload |
| real queue, producer succeeded | `TestReducerContentionGateSecurityAlertReconciliationGivesUpVisiblyWithoutAFence` | unmatched at attempt 3, no give-up on the result or the write |
| reader | `TestDecodeSecurityAlertReconciliationRowSurfacesTheImpactWaitGiveUp` | give-up row carries the field, genuine row omits it |
| finding commits between load and probe | `TestSecurityAlertReconciliationReadsSettlementBeforeTheEvidenceLoad` | old order wrote the stale row untagged (`failure class = "" (err <nil>)`); probing before the load defers with the non-counting class and writes nothing |
| stale decision with dependency evidence | `TestSecurityAlertReconciliationStaleDecisionDoesNotWaitOnImpact` and the writer test | not stamped, does not defer; the mutant that drops the `unmatched` guard fails both |

## Settlement is read before the load

The review found a window on the cap attempt: the evidence loaded first and
the producer probe ran later, so a finding that committed and acked in between
was missing from the decisions while the probe read settled. The cap attempt
now probes once before any load. Impact acks only after its finding commits:
`SupplyChainImpactHandler.Handle` calls the writer, which commits in its own
transaction (`go/internal/reducer/supplychain/core/writer_retract.go`,
`runSupplyChainImpactTx`, `tx.Commit`), and the reducer service acks the item
only after `Handle` returns (`go/internal/reducer/service_batch.go`,
`executeAndReport` then the batch ack). A settled reading before the load
therefore means every finding is visible to the load.

## Performance Evidence

No-Regression Evidence: the producer probe gains two `count(*) FILTER` terms
with the same `WHERE`. Measured on a throwaway `ci-postgres-bookworm`
(PostgreSQL 18) with all 248 migrations and 351,201 `fact_work_items` rows
(2,000 scopes, 5 generations, 35 domains, plus one 1,201-row
`supply_chain_impact` generation), three `EXPLAIN (ANALYZE, BUFFERS)` runs
each:

| Generation | Plan before and after | Buffers before | Buffers after | Execution before (ms) | Execution after (ms) |
| --- | --- | --- | --- | --- | --- |
| 1 row | Aggregate over Index Scan `fact_work_items_scope_generation_idx` | 4, 4, 4 | 4, 4, 4 | 0.045, 0.025, 0.020 | 0.022, 0.019, 0.018 |
| 1,201 rows | same | 87, 87, 87 | 87, 87, 87 | 0.408, 0.345, 0.344 | 0.376, 0.379, 0.386 |

The host is shared, so the times are not a throughput claim; the plan and the
buffer counts are the evidence. The probe still runs once per pass with a
decision waiting on impact evidence. No lock, claim, or lease path changed.

## Observability

Observability Evidence: `eshu_dp_security_alert_reconciliation_impact_wait_total`
gains `outcome="producer_settled"`; the settled INFO line and the give-up WARN
line carry `impact_total`, `impact_succeeded`, and `impact_failed`; the
persisted `impact_wait_outcome` is on the API and MCP row. Reference:
`docs/public/reference/telemetry/security-alert-reconciliation-impact-wait.md`.

## Not covered

- Whether a superseded producer can hold back the reconciliation of its own
  generation in a deployed run. Such a row keeps the generation unsettled, so
  it reads as a give-up.
- A deployed run with a real settled producer.
- Rows written by a pre-#8956 binary, including during a rolling deploy, carry
  no field; an untagged `unmatched` row from such a generation is not proof of
  a genuine no-match.
