# Advisory Group Graph Writes

The `advisory_group_projection` reducer domain (#10411) writes one thin
`AdvisoryGroup` anchor node per alias-connected advisory group and links
each affected repository to it:
`Repository-[:AFFECTED_BY]->AdvisoryGroup`. The domain reconciles one
group per intent from `supply_chain_impact_canonical_winners`, never
from raw advisory facts. It is a membership-only cache over the winners
read model; the PostgreSQL read path remains the CVE query surface.

## Schema

| Statement | Backend | Purpose |
| --- | --- | --- |
| `advisory_group_id`: unique `id` on `AdvisoryGroup` | Neo4j (constraint); NornicDB registers the single-property lookup | The MERGE key. The id is the #10407 group id copied verbatim from winners (`cve_id`, else `advisory_id`). |
| `repository_id`: unique `id` on `Repository` (pre-existing) | both | The edge source anchor. The writer MATCHes it and never MERGEs it, so a winner whose repository is absent from the graph writes nothing. |

`AdvisoryGroup` and `AFFECTED_BY` are new names: no other writer MERGEs
the label or the relationship type. The node holds only membership:
`id`, `cve_ids`, `advisory_ids`, and `member_count`, identical from
every scope, so concurrent writers write the same bytes. There is no
`evidence_source`, scope, generation, severity, EPSS, KEV, fix, or
suppression state on the node or the edge. Edge identity is
`package_id`; `ecosystem` and `purl` are its only mutable properties.
A group whose qualifying winners set is empty does not exist.

## Write and retract shapes

All four statements live in
`go/internal/storage/cypher/edge/writer/advisory_group.go` and anchor on
`AdvisoryGroup {id: $id}` (unique seek) or `Repository {id: ...}`.

- Upsert: `UNWIND $rows` (500-row batches), `MATCH` the `Repository`,
  `MERGE` the group node on `id`, `SET` the membership properties,
  `MERGE` the edge on `package_id`, `SET` `ecosystem` and `purl`.
- Stale-edge retract: expand the group's `AFFECTED_BY` edges and delete
  every (repository, package) pair outside the desired keep list. The
  check is pair-exact, so a cross pair never survives on the strength of
  its halves.
- Edgeless cleanup: after a retract, delete the node when its anchored
  degree reaches zero.
- Empty retract: for a group with no qualifying winners, delete its
  `AFFECTED_BY` edges, then the node, in one anchored statement.

Claims partition by group entity key in the reducer queue conflict
fence, so one group never reconciles concurrently while disjoint groups
drain in parallel. A claim never ack-completes before the winners
watermark passes its enqueue time.

## Measured plans

Performance Evidence: pinned Neo4j image `neo4j:2026-community` (server
2026.08.1), one throwaway container, 10,000 seeded `Repository` nodes
with the production DDL applied. The upsert plans `MergeUniqueNode` on
`advisory_group_id` plus `NodeUniqueIndexSeek(Locking)` on
`Repository(id)`; a 500-edge batch PROFILEd at 368 ms and 4,519 db hits.
At 1,000-edge supernode degree the stale retract runs `Expand(All)`
from the unique seek and deleted 990 stale edges in 82 ms. No held
shape scans the `AdvisoryGroup` label. The writer, handler, and seeded
Postgres-to-graph parity proofs are
`TestBoltAdvisoryGroupReconcileIsIdempotentAndRetracts` (live Neo4j),
`TestAdvisoryGroupWinnersQualifyingLive` (live PostgreSQL), and
`TestAdvisoryGroupParityLive` (both backends). The full evidence record
is `docs/internal/evidence/10411-advisory-group-projection.md`.

Observability Evidence: the domain records
`eshu_dp_advisory_group_projection_groups_total{domain,outcome}`,
edge upserted/retracted and node-deleted counters,
`eshu_dp_advisory_group_projection_reconcile_duration_seconds`, and
`eshu_dp_advisory_group_projection_lag_seconds`; see the operator
reference in `docs/public/reference/telemetry/advisory-group-projection.md`.
