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

That last point is a deliberate design choice, and it is worth being explicit
about. Eshu's recovery code holds no graph write credential at all — the recovery
package re-enqueues queue work and nothing else. The runtime admin routes that
expose it are unauthenticated by design, served on an internal admin port. Adding
"delete every node" to that surface would mean an unauthenticated request could
destroy the graph, and an environment-variable safety catch would only narrow
that window rather than close it. The wipe stays a step you take with the same
tools you use to manage the volume in the first place.

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
{"status":"recovered","enqueued":847,"duplicate":false,
 "idempotency_key":"dr-rebuild-2026-08-12"}
```

The route needs an admin token — one with all scopes, not a scoped token. It
also insists on a `reason` and an `idempotency_key`, both recorded in the
`admin_replay_requests` ledger, so the rebuild leaves an audit trail.

Pick a fresh `idempotency_key` per rebuild attempt. Reusing one from a *scoped*
recovery is refused with a 409 rather than quietly replaying that recovery's
much smaller outcome.

### 7. Watch it drain

```bash
watch -n 10 "curl -fsS \
  http://localhost:${ESHU_HTTP_PORT:-8080}/api/v0/index-status | jq .queue"
```

You are waiting for `pending`, `retrying`, `failed`, and `dead_letter` all at
zero. That is the rebuild's terminal state and the number to time.

### 8. Verify

```bash
curl -fsS "http://localhost:${ESHU_HTTP_PORT:-8080}/api/v0/index-status" \
  | jq '{status, queue}'
```

Then run at least one real query. `status=healthy` with a zeroed queue says
projection finished; it does not say the answers are right.

## If the rebuild is interrupted

Run step 6 again with a **new** `idempotency_key`. That is the whole recovery.

Interruption is safe by construction rather than by good luck. Each queued item
gets an id derived from its scope and generation, so re-enqueueing the same
generation updates the row that is already there instead of adding a second one.
Work that finished stays finished; work that was in flight when the process died
returns to `pending`; work that never started is unaffected. Running the command
five times leaves the same queue as running it once.

You do not need to wipe again before restarting. The graph is partially built,
`MERGE` is idempotent, and the remaining work fills in the rest.

## How long it takes

See [Performance SLO contract](../reference/performance-slo-contract.md) for the
measured `graph_rebuild_seconds` figure, the corpus it was measured on, and what
that number does and does not let you conclude about your own deployment. Size
your recovery time objective against a measurement from your own corpus, not
from the reference one.

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
