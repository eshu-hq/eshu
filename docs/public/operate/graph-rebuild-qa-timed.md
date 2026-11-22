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

1. The winners contract the rebuild must reproduce is on QA: the #10880 PR-A
   (#11188, winners contract version 2) is merged and pinned on the QA build,
   and its winners full resweep has recorded the contract. The authority is
   `contract_version` in `supply_chain_impact_winners_materialization`: it
   must equal the binary's `SupplyChainImpactWinnersContractVersion`. The log
   line `supply chain impact winners full resweep recorded the contract` only
   corroborates it: the instance that stored the full logs it, no other
   instance does, and logs rotate (see
   [supply-chain impact winners telemetry](../reference/telemetry/supply-chain-impact-winners.md)).
   Until the contract is recorded, the first full resweep is still a multi-hour
   reducer rewrite (about 6,400 s, measured on QA on 2026-11-21 under the
   earlier pin). A full records its contract in the transaction that commits
   it, so a restart before the commit discards the work. A rebuild timed now
   would mix two workloads and measure the wrong graph. A winners full that is
   in flight is a NO-GO. The later #10880 slices (B1 through PR-D) do not gate
   the drill.
2. No sibling lane holds the QA stack (check the epic #10860 drive claims and
   the [QA drill window](qa-drill-window.md) calendar).
3. The window is booked under the QA drill window with a named drill owner.
4. Pre-wipe evidence from the next section is captured fresh in the window.
   Numbers from an earlier day are stale.
5. Pre-existing queue is dispositioned. Snapshot `fact_work_items GROUP BY
   status` before the window; every pre-existing `dead_letter` row is
   dispositioned (replayed or named-accepted with owner sign-off) BEFORE the
   window, never silently deleted. At booking, read the deployment
   environment on the reducer and confirm the poison-liveness auto-retry switch
   (see [Service Runtimes](../deployment/service-runtimes.md)) is unset or
   false; if it is true, set it false before the window. That sweep re-drives
   old dead letters, and one that fails again counts in the cohort below. The
   timed stop below is defined on the rebuild cohort, not on the raw queue
   totals.
6. A restorable copy of the graph exists and has been proven to load, per
   [Restorable copy and abort path](#restorable-copy-and-abort-path). The
   graph volume's storage class deletes the disk with its claim, and this
   environment has no VolumeSnapshot support, so the wipe is otherwise
   one-way. The owner has also confirmed, at booking, what an abort does
   with the queue: the default in that section (accept the restored graph, no
   replay) or a different ruling.

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
`failed`/`dead_letter` rows in the cohort. Any new cohort dead letter during
the drain is a failed run, not a timing result, and the run aborts (see
[Abort and restore](#restorable-copy-and-abort-path)). No owner decision
happens mid-run: sign-off for classes that dead-letter without a rebuild is
given before the window under criterion 5.

The cohort is every row that matches this predicate. `:'t_submit'` is the
database clock, read with `SELECT clock_timestamp()` just before you submit
`recover-generations` (a minute earlier is also safe: the workers are stopped
until the submit, so nothing can dead-letter in between). Do not use the
operator's own clock.

Record that value, then run the poll with the value passed in as a psql
variable. `psql -c` does not interpolate `:'t_submit'`, and a `\gset` value
does not survive separate `kubectl exec` calls, so a `-c` poll would error and
count as an abort. Save the query below as `poll.sql` and run:

```bash
psql -v t_submit='<recorded clock_timestamp>' -f poll.sql
```

```sql
SELECT domain, failure_class, count(*)
FROM fact_work_items
WHERE status IN ('failed', 'dead_letter')
  AND updated_at >= :'t_submit'
  AND (domain, COALESCE(failure_class, '')) NOT IN
      (('advisory_group_projection', 'advisory_group_winners_watermark_stale'))  -- example row
GROUP BY 1, 2;
```

The `NOT IN` list is the pre-window sign-off, one `('domain', 'class')` row
per pair. The row shown is the real 2026-11-22 pair, only an example: replace
it with your own signed-off list. With no signed-off pairs, delete the whole `AND (domain, ...) NOT
IN ...` line: an empty `NOT IN ()` is a syntax error in Postgres.

A row the rebuild never touched keeps its old `updated_at`, so the
pre-existing dead letters from criterion 5 stay outside. A class that
dead-letters on QA with no rebuild running is excluded only if it was named
in the pre-window disposition as a `(domain, failure_class)` pair with the
rate measured over the 24 hours before the window. On 2026-11-22 the
`advisory_group_projection` / `advisory_group_winners_watermark_stale` class
grew by 695 rows in one day; without that sign-off it would fail every run.
Every other pair counts. Poll the predicate every minute during the drain; one
row is an abort, and so is a poll that errors: an error is not a pass.

## Kubernetes execution shape

The Compose procedure applies with these substitutions. Resolve every
discovery-gated step live; workload names below are examples from the 2026-11-21
stack, not pins.

1. Discover every graph writer live: list workloads whose environment carries
   `NEO4J_URI` or `ESHU_GRAPH_BACKEND`, and scale each writer (ingester,
   projector, resolution engine, and any other workload that writes canonical
   graph rows) to zero. Keep the API and read-only MCP up by name: the API
   carries the same variables but serves the read path and the submit step
   below, so it must not match this predicate. Anything that writes canonical
   graph rows must be stopped, or the rebuild races it. Recheck here that the
   reducer's poison-liveness auto-retry switch is still unset or false (entry
   criterion 5).
2. Take and prove the offline graph copy first (copy steps 1 to 5 in
   [Restorable copy and abort path](#restorable-copy-and-abort-path); entry
   criterion 6). Then scale the Neo4j workload to zero (a Deployment or
   StatefulSet, per the kind in the chart manifest; it is already at zero
   after the copy), delete and recreate only the `eshu-neo4j-data` PVC from
   the chart manifest, scale back to one, and wait for Bolt to answer. Never
   delete `eshu-neo4j-dump`.
3. Force schema reapply through whichever workload runs migrations in this
   environment, per the schema step in
   [Rebuild the graph from facts](graph-rebuild-from-facts.md). Confirm it
   took effect before continuing: read the migration workload's logs for
   `bootstrap.graph.(applied|force_reapply)` and require `graph schema
   applied`. `graph schema already applied` means the force-reapply variable
   did not reach the container — leaving it out is a silent failure that
   writes every node with no indexes and no constraints, appearing to work
   while slow and unenforced. Unset the variable afterwards: on a graph that
   survived, re-running `CREATE CONSTRAINT` costs minutes per constraint.
4. Scale the API back up if it was stopped and wait for `/health` to answer
   before submitting — the submit below POSTs to the API while projection
   workers stay stopped, which is what makes the recovery lease fence
   deterministic. Submit `recover-generations` with `all_scopes: true`, a
   fresh `idempotency_key`, and an admin token. Record `enqueued`,
   `skipped_scopes`, and the five dedup counters from the response. The clock
   starts here.
5. Scale the writers back up and watch both queues drain. Sample
   `kubectl top pod` for the Neo4j pod every minute for the peak-memory row,
   and scrape the `eshu_dp_neo4j_query_duration_seconds` histograms at start
   and end for the latency rows.
6. Verify: zeroed queues, then the pre/post identity comparison and at least
   one real query, per the Compose procedure.

## Restorable copy and abort path

The Neo4j edition here has no online backup, the storage class
(`gp3-fast`) deletes the disk when its claim is deleted, and the cluster has
no VolumeSnapshot resource type. Execution step 2 therefore destroys the only
copy of the graph. Take an offline dump to a new claim before it, prove the
dump loads, and keep it until the run passes. Do the copy steps in the window
with every graph writer at zero (execution step 1) and before the wipe
(execution step 2). The commands come from `neo4j-admin database dump --help`
and `database load --help` on the `neo4j:2026.08.1-community` image that QA
runs; the dump and load runs themselves are NOT_CHECKED until the drill
executes them.

**Copy steps**

1. **Record the pre-dump counts.** With Neo4j still up and writers at zero,
   run `cypher-shell` for node count, relationship count, per-label counts for
   the ten largest labels, and the output of `SHOW INDEXES` and `SHOW
   CONSTRAINTS` (name and state). These are the identity targets for the
   verification load and for the restore.
2. **Create the dump claim `eshu-neo4j-dump`.** Use the same storage class,
   50 GiB (the store is about 7.4 GiB, the reference table above),
   `ReadWriteOnce`. Create it outside the chart and Argo CD: no
   `app.kubernetes.io/instance` label and no Argo tracking annotation, so no
   sync prunes it. The class deletes the disk with the claim, so the wipe
   deletes only `eshu-neo4j-data`. The claim binds on first use.
3. **Stop Neo4j.** Scale the Neo4j workload to zero and wait for the pod to
   exit. A database mounted by a running server cannot be dumped.
4. **Dump in a one-shot pod.** Run a pod from the same image and digest as the
   live Neo4j container, with the same `securityContext` (user and group
   7474, `fsGroup` 7474, read-only root filesystem, no added capabilities).
   Mount `eshu-neo4j-data` at `/data` and `eshu-neo4j-dump` at `/dump`. The data
   volume is zonal, so schedule the pod in the volume's zone. The vendor docs
   say the dump target must not be world-readable or world-executable, and a
   fresh volume root usually is. So dump into a subdirectory created with mode
   0750, and use that same path in every command below and in the restore:

   ```bash
   mkdir -m 0750 /dump/graph
   neo4j-admin database dump neo4j --to-path=/dump/graph --verbose
   neo4j-admin database load neo4j --from-path=/dump/graph --info
   sha256sum /dump/graph/neo4j.dump
   ```

   Record the file size, the `--info` output (file count, byte count, format),
   and the digest. The dump covers the `neo4j` database only. It carries no
   users or roles (vendor docs). NOT_CHECKED: whether 2026.08.1 refuses or only
   warns on a looser target mode.
5. **Prove it loads.** Create a second new claim, `eshu-neo4j-verify` (30
   GiB). Run a scratch pod from the same image, with the same user, group and
   `securityContext` as copy step 4 (`/dump/graph` is mode 0750 with group
   7474, so another identity cannot read it), with `eshu-neo4j-verify` at
   `/data` and `eshu-neo4j-dump` at `/dump` (read-only). Load, then start
   Neo4j in that pod with `NEO4J_AUTH=none`, a 4 GiB heap, a 16 GiB page cache,
   and no Service, so only a `kubectl exec` can reach it:

   ```bash
   neo4j-admin database load neo4j --from-path=/dump/graph --overwrite-destination=true
   ```

   If `SHOW DATABASES` on the scratch server lists no `neo4j`, run `CREATE
   DATABASE neo4j` on the `system` database and record that the step was
   needed; NOT_CHECKED whether Community needs it after a load into a fresh
   claim. The verification passes only when every number from copy step 1
   matches and every index and constraint is `ONLINE`. Delete the scratch pod
   and the `eshu-neo4j-verify` claim afterwards.
   **If verification fails**, do not wipe. Scale Neo4j back to one against the
   untouched `eshu-neo4j-data`, scale the writers back to their previous
   counts, delete the scratch resources, keep `eshu-neo4j-dump` for diagnosis,
   stop the window, and post the mismatching numbers. A dump that was never
   loaded is not a backup.
6. **Optional second copy.** Before deleting `eshu-neo4j-data`, set the bound
   PersistentVolume's `persistentVolumeReclaimPolicy` to `Retain`, so the disk
   outlives its claim. This costs one idle 500 GiB volume, which someone must
   delete by hand after the run. Re-attaching a retained volume is NOT_CHECKED
   and is a fallback, not the restore path.

Time budget. The drill has not measured either number. Plan 45 minutes for
copy steps 1 to 5 (claim binding, scheduling, dump, load, counts) and 30
minutes for a restore, and replace both with the measured values after the
first run. The volume is provisioned for 16,000 IOPS and 1,000 MB/s, so the
dump and the load are not expected to be the long pole. The 24-hour cap runs
from the wipe (execution step 2) to a finished restore or a passing run. The
copy steps come before the wipe and do not count against it.

**Abort and restore.** Abort at the 24-hour cap, or at once when the cohort
predicate in [The clock](#the-clock) returns a row. Then restore:

1. Scale every graph writer to zero with the same predicate as execution step
   1, and Neo4j to zero.
2. Re-run `sha256sum /dump/graph/neo4j.dump` in a pod with `eshu-neo4j-dump` at
   `/dump` and require the digest recorded in copy step 4. Then, in a pod with
   `eshu-neo4j-data` at `/data` and `eshu-neo4j-dump` at `/dump` (read-only),
   run the same user, group and `securityContext` as the live container:

   ```bash
   neo4j-admin database load neo4j --from-path=/dump/graph --overwrite-destination=true
   ```

3. Scale Neo4j to one and wait for Bolt to answer. Do not force a schema
   reapply: the dump carries the schema, and the load restores it with its
   indexes and constraints. Read `SHOW INDEXES` and `SHOW CONSTRAINTS`
   instead. The `system` database on the claim, with its users and roles, was
   not touched by the rebuild.
4. Compare every number from copy step 1, then scale the writers back up.
5. Keep `eshu-neo4j-dump` until the owner confirms the graph. Then delete it
   (50 GiB, and the class deletes the disk with the claim). Do the same on a
   passing run once the evidence is recorded.

**What an abort does with the queue (default).** The rebuild marks
`fact_work_items` rows `succeeded` for a graph that the restore discards. The
default is to accept the restored graph as it was at the pre-wipe stop and not
to replay those scopes: the dump's `--info` output and the count comparison are
the evidence that it matches. A replay needs the owner's ruling before the
window, recorded under entry criterion 6.

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
