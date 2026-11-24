# ci_cd completion change gate (#10972 PR-4)

A ci_cd_run_correlation success used to emit a cross-scope completion event on
every ack (`reducer_queue_ack.go`, the ci_cd single and batch ack statements).
The completion fan-out then re-pended every settled supply_chain_impact item,
the only consumer of this domain's completion (`reducer/crossscope/dependencies.go`).
The reality live test `TestUnchangedCICDSuccessRePendsNoSupplyChainImpactItemLive`
measured that on the coordinator's run: 30 of 30 settled findings items
re-pended, with the fan-out enqueueing 30, for a ci_cd pass that wrote nothing.

## The gate

- **Verdict.** The ci_cd writer computes a changed verdict inside its own
  transaction (`cicdrun/completion_write.go`). It reads the rows it is about to
  overwrite by fact id and compares a digest of the fields supply_chain_impact
  reads (`cicdrun/completion_change.go` `completionDigestFields`).
  - Those fields come from `supplychain/core/deployment_context.go`
    (supplyChainDeploymentContextFromEnvelope), the filter expansion in
    `core/active_filter.go`, and `core/criteria_bound.go`.
  - Per-pass fields are left out: intent id, cause, generation id, evidence
    fact ids and source layers.
  - The following count as changed: a write with no rows (no baseline), a new
    fact id (a new generation always has new fact ids), a revived tombstone,
    or a different digest.
  - The writer never tombstones rows. An outcome downgrade (exact to rejected)
    is its form of a retraction, and it changes the digest.
- **Owed marker.** A changed write sets `payload.cicd_completion_owed` on the
  pass's own item in the same transaction, fenced on the lease and the attempt
  like the ack. A zombie pass therefore cannot commit rows or a marker.
- **Ack.** The single ack binds the verdict (`cicdrun.ShouldEmitCompletion`; a
  result without the verdict emits). The batch ack binds the ids that earned
  an event, and an item with no paired result emits (the #8284 identity model).
  - Either ack also emits for an item that carries the owed marker, and clears
    the marker.
  - An unchanged success still acks to succeeded and adds nothing to the event.
  - The epoch bump stays unconditional, so the migration 093 trigger still
    covers only old-reducer acks (rolling upgrade).

## Edges

- **Commit, then crash before the ack.** The marker survives the crash. The
  retry's own rewrite is identical, so its verdict is unchanged, but the marker
  makes its ack emit.
- **Two ci_cd passes of different items.** Each reads and writes only its own
  fact ids and its own work item. There is no shared row, and the lock order is
  unchanged.
- **Two passes of the same item.** The queue lease allows one live claim. A
  zombie's marker UPDATE matches zero rows, so its transaction rolls back.
- **Verdict scope.** The verdict covers the rows this pass writes. Rows a
  generation wrote earlier and this pass did not re-derive stay active and
  unchanged, so a consumer sees no change from them.

## No-regression

No-Regression Evidence: the write path adds one read by fact id, on the `fact_records` primary key,
inside the transaction the write now runs in. It also adds one UPDATE of the
pass's own work item, and only when rows changed. Before, the same rows were
upserted in autocommit batches.

The ack adds one primary-key subselect for the marker and a `payload - key` on
the row it already updates. No lock is added, no lock order changes, and no
claim statement changes.

The removed cost is the completion fan-out after every unchanged ci_cd success,
which was up to the whole settled supply_chain_impact backlog per event (30 of
30 in the live run). Measured local proof (coordinator, disposable PostgreSQL):

- the gate's live tests in `storage/postgres/supply_chain_impact_cicd_completion_*_live_test.go`
- `TestUnchangedCICDSuccessRePendsNoSupplyChainImpactItemLive`, red before the
  gate and green after

## Observability

Observability Evidence: the new signal and the existing ones below.

- `eshu_dp_cross_scope_completion_suppressed_total{domain}` counts the ci_cd
  passes whose write changed nothing a consumer reads. A pass with a pending
  owed marker still emits, so the counter can overstate suppression by those
  rare retries.
- The `cicd_completion_changed` result sub-signal (1 or 0, absent when there is
  no verdict) rides the reducer success line.
- The existing completion fan-out signals show the re-pends that remain.
