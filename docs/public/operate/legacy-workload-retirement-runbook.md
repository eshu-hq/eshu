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
- The route is supported on Neo4j only. On any other graph backend it answers
  `501` to dry runs and execute alike, with a fixed message.
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
| `no_twin` | no scoped twin the same repository defines; also a name or repository id with leading or trailing whitespace, which has no twin because the delete matches byte for byte | wait for workload materialization, or investigate; deleting would lose the only node. For a padded name or repository id, retirement cannot remove it: see the note after this table and #8903 |
| `relationships` | an edge outside the safe set; `relationships` shows counts by type | decide whether that evidence lives on the twin; remove it deliberately or leave the node |

A node whose name or repository id has leading or trailing whitespace is always
reported `no_twin`, and the readiness check does not count it, so readiness can
pass while it is still there. A padded repository id leaves the name clean, so
that node still matches a by-name selector and makes the service name
ambiguous. A padded name does not match the bare name (`svc`), because the name
probe is exact. A name padded only at the start matches only its exact handle
(`workload: svc`). A name padded at the end, or at both ends, matches no
selector at all, because selectors are trimmed on entry.

This is a known limit, tracked in #8903. Fixing the value at the source writes a
clean scoped node but does not remove the old one: the padded node has no twin,
stays `no_twin`, and retirement cannot delete it. It needs a manual, reviewed
removal.

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
`501` to dry runs and execute alike. A reducer with no graph writer wired
answers `503` to an execute; dry runs still work there.

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
request lock is per process and does not span replicas. A second run deletes
nothing.

The reducer records the `eshu_dp_workload_legacy_retirements_total{outcome}`
counter, an INFO line per request, a WARN line per node it kept, and a governance
audit event. A WARN `legacy workload retirement deleted fewer nodes than were
eligible` means a node changed under the delete, so re-run the dry run. An ERROR
`legacy workload retirement failed` means the delete statement itself failed. A
WARN `legacy workload retirement audit append failed` means the audit event was
lost; the retirement itself is unaffected.

## 5. Verify

Graph reads right after a write can lag. Re-run the dry run until it lists no
eligible candidates, and re-check readiness. Do not stop at the first empty read.

- `workload_identity_leftovers` turns to `pass` once no candidate with a scoped
  twin remains. A node you deliberately keep because of `relationships` still
  counts and keeps the check failing; a node blocked by `has_instances` or
  `no_twin` does not count.
- A by-name story lookup for an affected service resolves to the scoped node.

## If a leftover comes back

Something is writing the old id. No current code does. A reducer or ingester
still on a pre-cutover binary is the usual cause; roll it forward, then retire
again.
