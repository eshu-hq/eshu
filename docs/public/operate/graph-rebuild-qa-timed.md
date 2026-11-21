<!-- docs-catalog
title: Timed Graph Rebuild on QA
description: The Kubernetes procedure for timing a full graph rebuild from facts on the QA environment, with the QA reference points it is measured against.
type: operate
audience: operator
entrypoint: false
landing: false
-->

# Timed graph rebuild on QA

This page times the procedure in
[Rebuild the graph from facts](graph-rebuild-from-facts.md) on a real
deployment. The Compose fixture corpus proves the mechanism runs (341 s for 67
scopes); it says nothing about what a rebuild costs at QA size. That number is
the recovery time the graph's sizing and its recovery objective rest on
(#10883).

**Status: procedure ready, timed run not yet executed.** The reference points
below are measured. The wall-clock rebuild at QA size is pending a drill window
with the entry criteria below. Do not quote a QA rebuild time until that run
lands.

## Entry criteria

All must hold, else NO-GO:

1. #10880 is merged. It changes what the graph must answer; timing the rebuild
   before it lands measures the wrong graph.
2. No sibling lane holds the QA stack (check the epic #10860 drive claims and
   the [QA drill window](qa-drill-window.md) calendar).
3. The window is booked under the QA drill window with a named drill owner.
4. Pre-wipe evidence from the next section is captured fresh in the window.
   Numbers from an earlier day are stale.
5. Pre-existing queue is dispositioned. Snapshot `fact_work_items GROUP BY
   status` before the window; every pre-existing `dead_letter` row is
   dispositioned (replayed or named-accepted with owner sign-off) BEFORE the
   window, never silently deleted. The timed stop below is defined on the
   rebuild cohort, not on the raw queue totals.

## QA reference points (2026-11-21, pre-rebuild)

Measured read-only against the QA `eshu` namespace. Re-capture every row in
the drill window before the wipe.

| Signal | Value | How |
| --- | --- | --- |
| Graph nodes | 1,295,209 | `MATCH (n) RETURN count(n)` via `cypher-shell` on the Neo4j pod |
| Graph relationships | 3,062,391 | `MATCH ()-[r]->() RETURN count(r)` |
| Graph store (`/data`) | 7.4 GiB | `du -sh /data` on the Neo4j pod |
| Largest store files | strings 2.9G, schema (indexes) 1.1G, propertystore 641M, relstore 128M, arrays 65M, nodestore 19M | `du -sh /data/databases/neo4j/*` |
| Neo4j edition | Community 2026.08.1 (no clustering, no online backup) | `CALL dbms.components()` |
| Neo4j pod usage | 7m CPU / ~39 GiB RSS | `kubectl top pod` |
| Neo4j pod reserved | 32 CPU / 224 GiB (request = limit) | deployment spec |
| Neo4j heap / page cache | 31g / 160g | QA overlay in the GitOps repo |
| Graph PVC | 500 GiB, ~1.5% used | `kubectl get pvc eshu-neo4j-data` |
| Postgres scopes | 12,363 rows in `ingestion_scopes` | read replica |
| Postgres facts | 92,918,239 rows in `fact_records` | read replica |
| Rebuild queue depth | pending 7,784; retrying 3,966; claimed 21; running 3; dead_letter 2,924; succeeded 247,924; superseded 9,734 | `GROUP BY status` on `fact_work_items` |
| Graph read latency | mean ~3.8 ms, p95 within 25 ms (n=199, core-api window; 164/199 within 5 ms) | `eshu_dp_neo4j_query_duration_seconds{operation="read"}` |
| Reducer graph write latency | mean ~5.2 ms (n=83, 2-day window) | `eshu_dp_neo4j_query_duration_seconds{operation="write"}`, reducer metrics |
| Ingester write-group latency | mean ~0.53 s (n=20, 25-minute window) | `eshu_dp_neo4j_query_duration_seconds{operation="write_group"}`, ingester metrics |

The read/write latency rows above are idle-window samples, not full-queue
measurements. The timed run re-captures them while the queue drains.

## The clock

From the [Performance SLO contract](../reference/performance-slo-contract.md#graph-rebuild-from-facts):
start when `POST /api/v0/admin/recover-generations` with `all_scopes: true`
returns, stop when both queues are terminal — nothing in `fact_work_items`
outside `succeeded`/`superseded`, and no `shared_projection_intents` row with
`completed_at IS NULL`. Wipe and schema reapply sit outside the measurement.

The stop is defined on the rebuild cohort: compare against the pre-wipe
snapshot from entry criterion 5. At stop there must be zero new
`failed`/`dead_letter` rows attributable to the rebuild. Any new dead-letter
during the drain is a failed run, not a timing result.

## Kubernetes execution shape

The Compose procedure applies with these substitutions. Resolve every
discovery-gated step live; workload names below are examples from the 2026-11-21
stack, not pins.

1. Discover every graph writer live: list workloads whose environment carries
   `NEO4J_URI` or `ESHU_GRAPH_BACKEND`, and scale each to zero. Reads (API,
   MCP) can stay up; anything that writes canonical graph rows must be stopped,
   or the rebuild races it.
2. Scale the Neo4j Deployment to zero, delete and recreate the
   `eshu-neo4j-data` PVC from the chart manifest, scale back to one, and wait
   for Bolt to answer.
3. Force schema reapply (`ESHU_GRAPH_SCHEMA_FORCE_REAPPLY=true` semantics from
   the Compose procedure) through whichever workload runs migrations in this
   environment, and confirm the applied marker before continuing.
4. Submit `recover-generations` with `all_scopes: true`, a fresh
   `idempotency_key`, and an admin token. Record `enqueued`, `skipped_scopes`,
   and the five dedup counters from the response. The clock starts here.
5. Scale the writers back up and watch both queues drain. Sample
   `kubectl top pod` for the Neo4j pod every minute for the peak-memory row,
   and scrape the `eshu_dp_neo4j_query_duration_seconds` histograms at start
   and end for the latency rows.
6. Verify: zeroed queues, then the pre/post identity comparison and at least
   one real query, per the Compose procedure.

## Evidence template

Record the run where the SLO number lives
([Performance SLO contract](../reference/performance-slo-contract.md#graph-rebuild-from-facts)):

- `graph_rebuild_seconds`, corpus (scopes, `fact_records`), backend image,
  machine class, `enqueued` / `skipped_scopes`, peak Neo4j RSS and CPU during
  the drain, read/write latency histograms over the drain window, and the
  post-rebuild identity comparison.
- Whether the time is acceptable against the #10862 target (ten times one
  organization: about 94,000 scopes and 840 million facts). No recovery time
  objective is committed anywhere; the run sizes the objective, it does not
  pass or fail one. If the number rules out rebuild-as-recovery at the 10x
  target, say what changes (reserved capacity, seeded standby, or the deferred
  replication items named in the rebuild page).

## Related pages

- [Rebuild the graph from facts](graph-rebuild-from-facts.md) — the procedure.
- [Graph Backend Operations](../reference/graph-backend-operations.md#sizing-neo4j-qa-measured) —
  the sizing guidance these numbers feed.
- [QA drill window](qa-drill-window.md) — booking and roles.
