# Day-stuck partial scope status signal (#10389)

## Problem

A `supply_chain_impact` scope that stays partial through the non-file byte
budget or the 8-round `active_expansion_rounds` cap keeps serving stale
findings with no status reason marking it degraded. The #9778 headroom
tracker is process-local by design: a scope another replica completed
stays partial here until this process handles it again, and a restart
empties it. The status degraded reason needs per-scope pass state every
replica and restart agrees on.

## Change

One row per scope in `supply_chain_impact_scope_pass_state` (migration
284), written in the impact write transaction beside the finding upsert
and the #7142 admission: a partial pass marks the scope stuck with its
identity-affecting causes and stamps `partial_since` when the scope was
not already stuck; a full complete pass clears it, while a scoped complete
leaves the state untouched because it re-derives only its stamped CVE
subset (#9779); the fencing-token guard (same `<=` rule as the admission)
orders passes by token, but a stale-generation pass admitted later draws a
higher token and can still overwrite current state (#10422). The status
snapshot serves stuck rows live; health reports `degraded` with one reason
per scope partial for 24 hours or more, naming the scope, the stuck age,
and the cause. A later full complete pass clears the row.

## Not in this slice

- The existing `partial_scopes` gauge and WARN line are unchanged.
- The longer-than-an-hour alert stays with the dashboard owner.

## Proof

No-Regression Evidence (#10389): one admitted pass issues exactly one
extra statement. The per-scope upsert is a single primary-key
`INSERT ... ON CONFLICT (scope_id) DO UPDATE`, constant per pass and
independent of finding count, inside the transaction the admission
already opened: 5 statements before, 6 after, pinned by
`TestWriteSupplyChainImpactFindingsLocksUpsertsThenRetracts` (lock,
admit, pass state, stored read, upsert, retract). A state-record failure
rolls the pass back, pinned by the extended
`TestWriteSupplyChainImpactFindingsRollsBackOnFailure`. The status read
adds one scan over the scope-cardinality pass-state table filtered on
`partial_evidence`, with no secondary index: the table holds one row per
scope and both the upsert and the read key on `scope_id`. Snapshot round
trips go 27 to 28, pinned by `TestStatusStoreReadRawSnapshot` and
`TestReadStatusSnapshotEvaluatesActiveWorkOnce`. No new lock, queue
stage, lease, or graph write on either path. No live-backend timing is
claimed: the focused proof ran without a live database, and the cost case
rests on the statement pins above, which break on any extra round trip.

Observability Evidence (#10389): no new metric or span. The existing
`eshu_runtime_health_state{state="degraded"}` gauge flips through the
reason, and the per-read `eshu_dp_status_snapshot_read_duration_seconds`
metric gains one bounded read label (`supply_chain_stuck_partials`),
pinned by `TestReadStatusSnapshotLabelsEveryRead`. The
`partial_scopes{cause}` gauge series, the success-line sub-signals, and
the WARN line are byte-identical, pinned by the untouched headroom and
truncation tests. The reason text and the `supply_chain_stuck_partials`
text/JSON drilldown are pinned by the threshold unit tests and the render
wire goldens.

## Operating it

| Signal | Reading |
|---|---|
| `supply_chain_stuck_partials` non-empty in `/admin/status` | A scope's last recorded pass was partial; the row names the scope, the causes, and when it first went continuously partial |
| `degraded` with `supply-chain impact scope ... partial for ...` | That scope has been partial for 24 h or more and keeps stale findings; find the pass in the reducer WARN line for the scope |
| Row gone after the next pass | A complete pass cleared it; the scope re-converged |
