<!-- docs-catalog
title: Rebuild the Graph From Facts
description: The disaster-recovery procedure for reconstructing Eshu's graph from preserved Postgres facts after a restore or a lost graph volume.
type: operate
audience: operator
entrypoint: false
landing: false
-->

# Rebuild the graph from facts

Eshu's graph is a projection. Every node and edge in it was derived from rows
that live in Postgres — facts, content, queue state, and the scope and
generation bookkeeping that says which facts are current. Nothing in the graph
is the only copy of anything.

That is the whole disaster-recovery answer. You restore Postgres with whatever
backup tooling you already trust, throw the graph away, and rebuild it. There is
no graph-to-Postgres reconciliation to perform and no split-brain to resolve,
because one side was never a source of truth.

**Read [What the rebuild does not restore](#what-the-rebuild-does-not-restore)
before you rely on this.** The rebuild is measured and it is incomplete: code
call and inheritance edges, ownership, and several correlation families do not
come back on their own today.

**Deliberately not on the menu:** graph-backend replication and multi-region
graph storage. Both are deferred until rebuild-from-facts is shown to miss a
real deployment's recovery time objective. They are the most expensive items on
the availability menu, and buying them before measuring the cheap answer would
be guesswork.

## When you need this

| Situation | Rebuild? | Wipe first? |
| --- | --- | --- |
| Graph volume or PVC is lost or will not reopen | Yes | Nothing to wipe |
| Postgres restored from backup, graph still running | Yes | **Yes** |
| Graph reachable but returning obviously wrong results | Yes | **Yes** |
| Individual scopes wedged, graph otherwise fine | No — use scoped recovery | No |

The wipe column is the one that catches people. Read the next section before
skipping it.

## Why a wipe, and when you can skip it

Rebuilding re-runs projection over the facts Postgres holds. Projection writes
with `MERGE`, which is idempotent — it will not duplicate a node — but it also
never deletes. So a rebuild over a surviving graph gives you everything the
facts describe *plus* everything the old graph already held.

Usually those are the same set and the extra costs you nothing. They are not the
same set when the graph is newer than the Postgres you restored, which is
exactly the case that sends you here. Say Postgres went back to Tuesday's backup
and the graph kept running until Thursday. Wednesday's repositories are in the
graph and are in no fact you now hold. A rebuild leaves them there, indefinitely,
and every query counts them.

So: wipe when the graph may hold state your restored Postgres cannot account
for. Skip the wipe only when the graph is already gone or already empty.

Wiping means recreating the graph's storage — the `nornicdb_data` Compose volume
or the NornicDB PVC — not issuing a delete query. Recreating the volume is faster
than deleting several million nodes in batches, and it is more complete: it
clears indexes and constraints too, which a delete sweep leaves behind. It also
keeps the destructive step where an operator can see it, rather than behind an
HTTP route.

That last point is deliberate. Eshu's recovery code holds no graph write
credential at all — the recovery package re-enqueues queue work and nothing else.
The runtime admin routes that expose it carry no authentication of their own;
they are protected by not exposing the admin port. Putting "delete every node"
there would put the graph one unauthenticated request from destruction, and an
environment-variable safety catch would narrow that window rather than close it.
So the wipe stays a step you take with the same tools you already use to manage
the volume.

## The procedure

The commands below use Docker Compose. On Kubernetes the shape is identical:
scale the writers to zero, delete and recreate the NornicDB PVC, scale back up.

### 1. Stop everything that writes to the graph

```bash
docker compose stop eshu mcp-server ingester resolution-engine projector \
  workflow-coordinator component-extension-collector webhook-listener
```

Leave `postgres` running. It holds the facts you are about to rebuild from.

### 2. Restore Postgres, if that is why you are here

Use your own backup tooling. Eshu does not ship a restore wrapper, and
`scripts/restore-eshu-backup.sh` exists only to say so.

If the graph is what failed and Postgres was never touched, skip this step.

### 3. Wipe the graph

```bash
docker compose rm -sf nornicdb
docker volume rm "$(docker compose config --format json \
  | jq -r '.name')_nornicdb_data"
docker compose up -d nornicdb
```

Skip this step only if the graph is already gone.

### 4. Reapply graph schema — do not skip this

```bash
ESHU_GRAPH_SCHEMA_FORCE_REAPPLY=true docker compose up db-migrate
```

`ESHU_GRAPH_SCHEMA_FORCE_REAPPLY` is not optional here, and leaving it out is a
silent failure rather than a loud one.

Eshu records "graph schema has been applied" as a row in Postgres, in
`graph_schema_applications`. That row is a claim about a graph the row cannot
see. You have just kept Postgres and wiped the graph, so the claim still matches
and schema bootstrap decides it has nothing to do — it returns before it opens a
connection to the graph at all. Your rebuild then writes every node into a
backend with no indexes and no constraints. It will appear to work. It will be
slow, and it will not enforce uniqueness.

Setting the variable tells bootstrap the marker is stale. Confirm it took effect
before continuing:

```bash
docker compose logs db-migrate | rg 'bootstrap.graph.(applied|force_reapply)'
```

You want `graph schema applied`. If you see `graph schema already applied`, the
variable did not reach the container and you have to fix that before going on.

Unset the variable afterwards. On a graph that survived, re-running
`CREATE CONSTRAINT` costs minutes per constraint, which is the reason the marker
skip exists.

### 5. Start the services back up

```bash
docker compose up -d
```

Wait for the API to answer:

```bash
curl -fsS "http://localhost:${ESHU_HTTP_PORT:-8080}/health"
```

### 6. Rebuild

```bash
curl -fsS -X POST \
  "http://localhost:${ESHU_HTTP_PORT:-8080}/api/v0/admin/recover-generations" \
  -H 'content-type: application/json' \
  -H "authorization: Bearer $ESHU_API_KEY" \
  -d '{
        "all_scopes": true,
        "reason": "graph rebuild from preserved facts after restore",
        "idempotency_key": "dr-rebuild-2026-08-12"
      }'
```

`all_scopes` re-enqueues projector work for every active scope that holds an
active generation. It exists because after a restore nobody has a scope list to
type, and it is the difference between a command you can run at 3 AM and an
afternoon of copying scope ids out of `psql`.

The response tells you how much work was queued:

```json
{"status":"recovered","enqueued":67,"duplicate":false,
 "idempotency_key":"dr-rebuild-2026-08-12",
 "scope_ids":["git-repository-scope:repository:r_9e291581", "..."]}
```

`enqueued` is the number of active scopes queued, and `scope_ids` lists them
(truncated above). Both come from a real run against the Compose fixture corpus,
so expect a much larger number on a real deployment.

The route needs an admin token — one with all scopes, not a scoped token. It
also insists on a `reason` and an `idempotency_key`, both recorded in the
`admin_replay_requests` ledger, so the rebuild leaves an audit trail.

On a stack running with `ESHU_AUTO_GENERATE_API_KEY=true` and no configured
key, the API generates one on first start and persists it under `ESHU_HOME`:

```bash
ESHU_API_KEY=$(docker compose exec -T eshu \
  sh -lc 'sed -n "s/^ESHU_API_KEY=//p" /data/.eshu/.env')
```

A rebuild also reports the dedup state it cleared — `reducer_work_deleted`,
`shared_intents_reopened`, and `readiness_phases_cleared`. After a wipe all three
should be non-zero; three zeros mean the rebuild will restore source-local
structure and nothing else. The response above was captured before those
counters existed, which is why it does not show them; the fields are described in
[Status and admin endpoints](../reference/http-api/status-admin.md).

Pick a fresh `idempotency_key` per rebuild attempt. Reusing one from a *scoped*
recovery is refused with a 409 rather than quietly replaying that recovery's
much smaller outcome. Reusing the key from a rebuild that already finished
returns that rebuild's `enqueued` and `scope_ids` with `duplicate: true`, but
not the three counters — the ledger does not store them. If you lost the first
response, count the effect in Postgres instead: pending `projector` rows in
`fact_work_items`, and `shared_projection_intents` with `completed_at IS NULL`.

### 7. Watch it drain

```bash
watch -n 10 "curl -fsS \
  -H 'authorization: Bearer $ESHU_API_KEY' \
  http://localhost:${ESHU_HTTP_PORT:-8080}/api/v0/index-status | jq .queue"
```

You are waiting for `pending`, `retrying`, `failed`, and `dead_letter` all at
zero. That is the rebuild's terminal state and the number to time.

### 8. Verify

```bash
curl -fsS "http://localhost:${ESHU_HTTP_PORT:-8080}/api/v0/index-status" \
  -H "authorization: Bearer $ESHU_API_KEY" \
  | jq '{status, queue}'
```

Then run at least one real query. `status=healthy` with a zeroed queue says
projection finished; it does not say the answers are right.

## If the rebuild is interrupted

Run step 6 again with a **new** `idempotency_key`. That is the whole recovery.

Run step 6 again with a **new** `idempotency_key`, then wait for the drain to
finish. Each queued item gets an id derived from its scope and generation, so
re-enqueueing the same generation updates the row that is already there instead
of adding a second one. Work that was in flight when the process died returns to
`pending`; work that never started is unaffected.

This has been measured, not assumed. On the Compose fixture corpus, killing the
ingester, projector, and resolution-engine mid-rebuild left 62 work items in
flight, 505 shared intents open, and a half-built graph of 522 nodes. After a
restart and a re-issued command, both queues drained to terminal with zero
dead-letter and zero failed rows, and the graph came back matching the
uninterrupted rebuild on every label and edge type — plus one `CALLS` edge the
uninterrupted run had missed, because those edges depend on nodes another domain
materializes and a single pass can run them in the wrong order.

Re-running is safe. A second pass adds what the first missed, and passes after
that change nothing: across four measured rebuilds the `EvidenceArtifact` set
settled at pass 2 and passes 3 and 4 added and removed exactly zero, compared by
node id rather than by count.

It is not free, though. Each call clears the dedup state for every generation it
covers, including reducer work the *current* rebuild has already finished, so
re-issuing mid-drain hands that work back to the queue and the rebuild takes
longer. Watch the drain first; re-issue when it has stopped making progress, not
because it is slow.

You do not need to wipe again before restarting. The graph is partially built,
`MERGE` is idempotent, and the remaining work fills in the rest.

## What the rebuild does not restore

A rebuild used to stop at source-local structure: it brought back 2,431 of 2,504
nodes and 2,905 of 3,289 relationships, with the whole call-graph, inheritance,
ownership, and correlation layers missing. That is fixed. A rebuild now restores
2,503 of 2,505 nodes and 3,286 of 3,288 relationships on the same corpus, and
every one of the seventeen reducer domains re-runs.

What is left is small, and it is worth knowing what each piece is.

**One cross-repository `CALLS` edge can come back missing on a single pass.**
A call from one repository into another needs both repositories' code nodes
committed. The shared-projection readiness gate waits only on the calling
repository, and the edge query matches nothing rather than waiting, so a pass
that drains the edge before the other repository is rebuilt silently writes
nothing. Measured: `CALLS` at 115 of 116, reproducible across three runs.
Re-running the rebuild recovers it — see
[If the rebuild is interrupted](#if-the-rebuild-is-interrupted), including the
warning about what else repeated runs do.

**`HANDLES_ROUTE` and `RUNS_IN` are intermittent.** These connect code symbols to
`:Endpoint` and `:Workload` nodes that a different domain materializes. Across
four measured rebuilds they came back at 0, 2, 4, and 0 out of 4. Waiting for the
shared edge backlog to drain is what makes a complete pass possible at all, but
it does not guarantee one. Re-running the rebuild recovers them.

**Same-named modules in different languages come back with the wrong language.**
A `Module` graph node is keyed on its name alone, so one node named `time` serves
Go and Python both, and its `lang` is whichever writer landed first. A rebuild
re-runs the writers in a different order, so the language can flip. This is not
caused by the rebuild — the same collision happens during ordinary indexing — but
a rebuild is where you will notice it. If you compare module counts before and
after, expect the totals to match while individual nodes disagree.

**Some of the remaining difference is not the rebuild at all.** Indexing the same
corpus twice does not produce byte-identical graphs. Three runs of this procedure
recorded pre-wipe totals of 2,506/3,294, 2,504/3,289, and 2,505/3,288, differing
in `EvidenceArtifact`, `Module`, and `Environment` — the same families that show
up in a rebuild comparison. When you compare a rebuilt graph against counts you
recorded earlier, expect a couple of nodes of noise from the indexer before you
suspect the rebuild.

The measurement, the per-label counts, and the domain-by-domain breakdown are in
`docs/internal/evidence/4594-graph-rebuild-from-facts.md`.

## How long it takes

**There is no published bound, and you should not infer one from this page.**

One measured run of the complete operation — both queues terminal — took 341
seconds (5m41s) for 67 scopes and 3,866 facts on the Compose fixture corpus. That
is a single sample on a 1.4 MB corpus, which is far below any realistic
deployment. It shows the mechanism runs. It is not a recovery time objective.

Earlier numbers on this operation (15 s before the fix, 20-25 s after) stopped
measuring when the work queue emptied, before the shared edge backlog finished.
That backlog has been seen idling for four minutes and then draining in one
burst, so those figures undercount the real rebuild by an amount that varies. Do
not compare them against the 341-second figure — they are not the same
measurement.

The direction of the fix's cost is solid even though the multiplier is not: the
rebuild now re-drives about 1,000 reducer work items and 600 shared intents
instead of a few hundred rows, because it is restoring layers it previously
skipped.

Size your recovery time objective against a measurement from your own corpus.
Run `scripts/verify-graph-rebuild-from-facts.sh` against it and use what it
prints. See
[Performance SLO contract](../reference/performance-slo-contract.md#graph-rebuild-from-facts)
for the conditions the reference number was taken under.

## Related pages

- [Hosted backup and restore proof](../deploy/kubernetes/backup-restore-proof.md)
  — the evidence packet a hosted restore drill produces. This page is the
  procedure; that one validates the summary of having run it.
- [Health checks](health-checks.md) — the shorter graph-data-loss sequence, for
  when the graph is simply gone and Postgres was never touched.
- [Runtime admin API](../reference/runtime-admin-api.md) — the scoped
  `refinalize` and `replay` routes, for recovering individual wedged scopes
  without a full rebuild.

`scripts/verify-graph-rebuild-from-facts.sh` runs this page end to end against a
Compose stack and times it. The measurement and the evidence it rests on are
recorded in
[Performance SLO contract](../reference/performance-slo-contract.md#graph-rebuild-from-facts).
