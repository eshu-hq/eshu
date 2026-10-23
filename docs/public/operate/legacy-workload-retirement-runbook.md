# Retire Leftover Name-Only Workload Nodes

After the workload identity change (#5385) a `Workload` node is keyed
`workload:<repo_id>:<name>`. A deployment that upgraded across that change and
missed the cutover's delete step still holds the old node, keyed
`workload:<name>`, next to the new one. A by-name service lookup then sees two
candidates for one service and refuses to guess, and the reducer skips edges as
`workload_handle_ambiguous`. The `409` answer marks the old candidate
`legacy_id: true`. That marker checks only the id shape; the dry run below
decides whether the node is safe to retire.

Hosted readiness reports this as the failed check `workload_identity_leftovers`
(failure class `legacy_workload_nodes`). This runbook retires exactly those
nodes, dry run first. Nothing retires automatically.

## What counts as a leftover

A node is eligible only when every clause holds:

1. Its id is exactly `workload:<name>` for its own `name`, and it has no
   `generation_id`.
2. No `WorkloadInstance` references it, by an `INSTANCE_OF` edge or by the
   instance's `workload_id` property.
3. A scoped twin exists: a node with id `workload:<repo_id>:<name>` for the same
   `repo_id` and name, with a `generation_id`, that the same repository
   `DEFINES`. A same-named node in a different repository is not a twin.
4. Every relationship on the leftover is a `DEFINES` from its own repository or a
   `SAME_NAME` to another `Workload`. Anything else (`DOCUMENTS`, `DEPENDS_ON`,
   `EXPOSES_ENDPOINT`, `USES`, `RUNS_ON`, and so on) blocks the node.

Two scoped nodes that share a name in different repositories are a genuine
ambiguity, not a leftover. The resolver keeps refusing to pick between them and
this procedure never touches them.

## Before you start

- You need the owner's explicit go-ahead and a reviewed dry run for any
  deployment you care about. Execute deletes graph nodes.
- Reach the reducer admin port from inside the network (for example by port
  forward). The admin routes are unauthenticated by design and must never be
  exposed outside it. Use `<reducer-admin>` below for that address.
- Execute is supported on Neo4j only. On any other graph backend the route
  answers `501` to an execute with a fixed message.
- Do not run this during the #5385 cutover or a rollback to a pre-cutover
  binary. The old binary writes name-only nodes again; retire only after the
  deployment settles on the new one.

## 1. Confirm the finding

`GET /api/v0/status/hosted-readiness` lists a failed `workload_identity_leftovers`
check. Its detail gives the count of leftovers that have a scoped twin.

## 2. Dry run

```bash
curl -s -X POST http://<reducer-admin>/admin/retire-legacy-workloads \
  -d '{"dry_run": true, "limit": 100}'
```

The response lists candidates with `eligible` and, for blocked nodes,
`blocked_reason`:

| `blocked_reason` | meaning | what to do |
| --- | --- | --- |
| `has_instances` | an instance still references the node | find why an instance keeps the old id; do not delete |
| `no_twin` | no scoped twin the same repository defines | wait for workload materialization, or investigate; deleting would lose the only node |
| `relationships` | an edge outside the safe set; `relationships` shows counts by type | decide whether that evidence lives on the twin; remove it deliberately or leave the node |

`truncated: true` means more candidates exist than one request examined. Page by
repeating the call after retiring, or name a subset with `"ids": [...]`.

A dry run examines at most 1,000 candidates in id order and has no cursor. When
the first 1,000 are all blocked, repeating the call returns the same blocked
nodes and makes no progress. In that case list candidate ids with a read-only
query against the graph (`MATCH (w:Workload) WHERE 'workload:' + w.name = w.id
AND w.generation_id IS NULL AND w.id > $after RETURN w.id ORDER BY w.id LIMIT
100`) and pass each page as `"ids"`.

The detection checks `WorkloadInstance` references (edge and `workload_id`
property) and the node's own relationships. It does not check other labels
that may name a workload only by a property, such as `CloudResource.workload_id`
or `Endpoint.workload_id`. Check those yourself for the ids you plan to retire.

## 3. Review

Check the eligible ids against the twin ids in the response. Use `ids` to retire
a first small batch. Confirm the blocked nodes are ones you intend to keep for
now.

## 4. Execute

```bash
curl -s -X POST http://<reducer-admin>/admin/retire-legacy-workloads \
  -d '{"dry_run": false, "limit": 100, "reason": "<why this is safe>"}'
```

`reason` is required and goes to the log. `dry_run` has no default: a body
without it is rejected. Each request retires at most 100 nodes in one short
transaction. The route runs on Neo4j only; on any other graph backend it answers
`501` to dry runs and execute alike.

Execute is irreversible. There is no undo; recovering a deleted node means
rebuilding it from facts (re-materializing the repository).

The statement takes each node's write lock and then re-checks every clause, so
it does not trust the dry run. What that covers, measured on Neo4j 2026.08.1:

- A change committed before the statement runs (a new instance, a new edge, a
  twin that lost its `DEFINES`) keeps the node.
- A writer that changes the node itself while the statement runs makes the
  statement wait; it then reads the committed change and keeps the node.
- A writer that creates an `INSTANCE_OF` or other edge to the node while the
  statement runs makes the two transactions wait on each other. Neo4j aborts
  one with a deadlock error: if it aborts the delete, nothing is deleted and the
  retry keeps the node; if it aborts the writer, its edge never existed.
- Not covered: a `WorkloadInstance` created with only a `workload_id` property
  naming the node, or the twin losing its `DEFINES`, while the statement runs.
  Neither touches the node, so the statement does not see it.

So run execute in a quiet window, and send it to one reducer pod: the reducer's
request lock is per process and does not span replicas. (Two delete statements
on the same id at once were probed on Neo4j: in five of five rounds exactly one
deleted the node; that is not a reason to run two pods.) A second run deletes
nothing.

The reducer records the `eshu_dp_workload_legacy_retirements_total{outcome}`
counter, an INFO line per request, a WARN line per node it kept, and a governance
audit event.

## 5. Verify

Graph reads right after a write can lag. Re-run the dry run until it lists no
eligible candidates, and re-check readiness. Do not stop at the first empty read.

- `workload_identity_leftovers` turns to `pass`.
- A by-name story lookup for an affected service resolves to the scoped node.

## If a leftover comes back

Something is writing the old id. No current code does. A reducer or ingester
still on a pre-cutover binary is the usual cause; roll it forward, then retire
again.
