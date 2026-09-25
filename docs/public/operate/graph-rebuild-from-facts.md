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
before you rely on this.** The measured rebuild restores the formerly missing
code-call and deployable-unit edge families, but it does not yet reproduce a
byte-identical graph. The remaining measured difference is in workload/platform
and deployment-evidence materialization, not the owned code-call or
deployable-unit lanes.

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

Wiping means recreating the graph's storage — the active `nornicdb_v132_data`
Compose volume or the NornicDB PVC — not issuing a delete query. Recreating the volume is faster
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
  | jq -r '.name')_nornicdb_v132_data"
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

### 5. Start the API, but keep projection workers stopped

```bash
docker compose start eshu
```

Wait for the API to answer:

```bash
curl -fsS "http://localhost:${ESHU_HTTP_PORT:-8080}/health"
```

Do not start the ingester, projector, or resolution engine yet. The recovery
transaction retires the active relationship generations before it re-enqueues
them, and its safety fence refuses to race a reducer holding a live lease.
Submitting the command while workers remain stopped makes that fence
deterministic. Start the workers only after the command is accepted.

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

`all_scopes` re-enqueues projector work for every recoverable scope: each active
scope through its active generation, and each failed scope through its newest
failed generation. A scope whose last projection attempt failed on the old graph
backend (a write timeout, for example) has no active generation, and the new
backend has not seen that failure, so the rebuild includes it. It exists because
after a restore nobody has a scope list to type, and it is the difference
between a command you can run at 3 AM and an afternoon of copying scope ids out
of `psql`.

The response tells you how much work was queued:

```json
{"status":"recovered","enqueued":67,"duplicate":false,
 "idempotency_key":"dr-rebuild-2026-08-12",
 "scope_ids":["git-repository-scope:repository:r_9e291581", "..."]}
```

Now release the queued work:

```bash
docker compose start ingester projector resolution-engine mcp-server
```

Start any optional graph-writing services enabled in your deployment at the
same point. If the request is refused because reducer work still holds a live
lease, leave the workers stopped, wait for that lease to expire, and retry with
a fresh `idempotency_key`; the failed key remains recorded as in progress so a
retry cannot re-drive the transaction ambiguously.

`enqueued` is the number of scopes queued, and `scope_ids` lists them
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

The response also carries `skipped_scopes`: the scopes the rebuild considered and
left out, with `total`, exact `by_reason` counts, and up to 10 `sample_scope_ids`
per reason (ascending). It is always present, with empty objects when nothing
was skipped. Read it before you call the rebuild complete; `total: 0` means every
scope in `ingestion_scopes` was queued. The reasons:

| Reason | Meaning |
| --- | --- |
| `no_recoverable_generation` | Failed scope whose generations are all superseded. Nothing in Postgres can be projected; the repository needs a new collection. |
| `newest_generation_not_failed` | Failed scope whose newest non-superseded generation is pending. That generation has its own projector work, so the rebuild does not queue it twice. |
| `no_active_generation` | Scope that is neither active nor failed, such as a first generation still pending. Its own projector work activates it. |
| `unknown_scope` | A named `scope_ids` entry with no `ingestion_scopes` row. Named requests only. |

The same counts are logged as `recover-generations completed` (Warn when
anything was skipped) and counted in `eshu_dp_recovery_scopes_skipped_total`. The governance audit event
(`admin_recovery_action`) records a rebuild that skipped scopes with reason code
`recover_generations_accepted_partial`, and a complete one with
`recover_generations_accepted`, so a partial rebuild stays distinguishable in the
durable audit ledger after logs rotate. The audit event carries no counts; the
response body and the log line do.
An idempotent retry (`duplicate: true`) does not repeat the report.

A rebuild also reports the dedup state it cleared — `reducer_work_deleted`,
`shared_intents_reopened`, `readiness_phases_cleared`, and `generations_retired`
(active relationship generations superseded so the re-projection never consumes
the prior wave's resolved rows as current truth). Inspect these counts against
the stack's prior state, not as four required non-zero postconditions:
`generations_retired=0` is valid when no relationship generation was active,
and another zero may mean there were no matching rows to reset. An unexpected
all-zero response on a populated stack calls for checking queue state and the
final graph identity comparison. The response above was captured
before those counters existed, which is why it does not show them; the fields
are described in
[Status and admin endpoints](../reference/http-api/status-admin.md).

Pick a fresh `idempotency_key` per rebuild attempt. Reusing one from a *scoped*
recovery is refused with a 409 rather than quietly replaying that recovery's
much smaller outcome. Reusing the key from a rebuild that already finished
returns that rebuild's `enqueued` and `scope_ids` with `duplicate: true`, but
not the four counters — the ledger does not store them. If you lost the first
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

Keep every projection worker stopped, then run step 6 again with a **new**
`idempotency_key`. The recovery transaction waits up to five minutes for a
hard-killed reducer's lease to expire, within the API's six-minute response
window. Start the workers only after the request is accepted, then wait for the
drain to finish. Each queued item gets an id derived from its scope and
generation, so re-enqueueing the same generation updates the row that is already
there instead of adding a second one. Work that was in flight when the process
died returns to `pending`; work that never started is unaffected.

This has been measured, not assumed. On the Compose fixture corpus, the workers
were killed only after graph rows existed while queue work was still active.
After the re-issued command was accepted and the workers restarted, both queues
drained to terminal with zero dead-letter and zero failed rows. The clean and
interrupted rebuilds each restored all 116 `CALLS` edges in one pass; the
fleet-wide canonical-repository readiness gate now holds cross-repository edges
until both endpoints can exist.

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
ownership, and correlation layers missing. That is fixed. In the latest clean
run, the pre-wipe graph held 2,529 node identities and 3,308 relationship
identities; the rebuild held 2,530 and 3,309. The identity differential was two
missing and three additional nodes, plus four missing and five additional
relationships. Every reducer domain re-ran, but that residual means this is not
a byte-identical restoration claim.

What is left is small, and it is worth knowing what each piece is.

**Cross-repository `CALLS` waits for every active code repository.** A call from
one repository into another needs both repositories' code nodes committed. The
shared-projection lane now checks fleet-wide canonical-repository quiescence
before writing, so a single clean or interrupted rebuild restores all 116 edges
on the measured corpus. A repository that never publishes its canonical phase
holds the lane visibly in readiness deferral instead of silently losing an
edge. Investigate the missing phase and pending queue row; do not re-run a
healthy rebuild merely to recover `CALLS`.

**Deployable-unit edges wait for canonical graph quiescence.** `HANDLES_ROUTE`
and `RUNS_IN` connect code symbols to `:Endpoint` and `:Workload` nodes that a
different domain materializes. The deployable-unit lane now waits for the
fleet-wide canonical phase before reading resolved relationships or writing
edges. The clean and interrupted v1.3.2 runs each restored all four edges in
both families; a missing prerequisite stays visibly deferred instead of being
silently accepted as an empty match.

**The remaining measured difference is outside the owned lanes above.** The
latest clean run was missing one `WorkloadInstance`, its `Platform`, and four
relationships, while adding two deployment `EvidenceArtifact` nodes, one
`Environment`, and five relationships. The earlier interrupted run lost no
identity and added three nodes plus nine relationships in workload-instance
materialization. The code-call and deployable-unit counts stayed complete in
both runs, while exact whole-graph identity parity remains unproved. Treat any
workload/platform or deployment-evidence delta as a convergence issue to
investigate, not as evidence that a missing code or deployable-unit lane is
expected.

The original baseline and the current measurement, including per-label and
domain-by-domain breakdowns, are in
`docs/internal/evidence/4594-graph-rebuild-from-facts.md` and
`docs/internal/evidence/6184-cross-repo-calls-readiness-and-resolver-ordering.md`.

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
