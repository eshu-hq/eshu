# #5714 / #5055 — CloudResource UNWIND row-key defaults: prove-theory-first shim

Issue #5714 (subsuming #5055) asked for a durable, shared-writer defense
against `canonicalCloudResourceUpsertCypher`'s (`go/internal/storage/cypher/
cloud_resource_node_writer.go`) `UNWIND $rows AS row ... SET r.<key> =
row.<key>` shape: a row map missing a key does not evaluate to `null` on the
pinned NornicDB backend, it persists a stringified `"row.<key>"` literal
instead. Two options were named — Option A (writer default-fill) and Option B
(Cypher `coalesce`) — with an explicit instruction to prove which one actually
works on the pinned backend before implementing either, since Option B's
viability depends on the exact `coalesce`-over-a-missing-map-key semantics
that is the broken behavior in the first place.

## The shim

A throwaway `_test.go` file (not committed) in `go/internal/storage/cypher`,
gated behind `ESHU_CLOUDRESOURCE_ROWKEY_PROVE_LIVE=1`, ran three Cypher shapes
against an isolated NornicDB instance built from the exact image the
repo-root `docker-compose.yaml` pins for local development
(`eshu-nornicdb-pr261:149245885258`, reporting `service=nornicdb
version=1.1.11` in its own startup log) on private ports (not the shared
`nornic-pin`/`nornic-probe-*` port set other concurrent sessions on this
machine were using — see `docs/internal/agent-guide.md`'s live-gate
contention notes):

```bash
docker run -d --name eshu-5714-nornic-probe -e NORNICDB_NO_AUTH=true \
  -p 17788:7687 -p 17475:7474 eshu-nornicdb-pr261:149245885258

cd go && ESHU_CLOUDRESOURCE_ROWKEY_PROVE_LIVE=1 ESHU_GRAPH_BACKEND=nornicdb \
  ESHU_NEO4J_URI=bolt://localhost:17788 ESHU_NEO4J_USERNAME=neo4j \
  ESHU_NEO4J_PASSWORD=x ESHU_NEO4J_DATABASE=nornic \
  go test ./internal/storage/cypher -run TestProveCloudResourceRowKeyTheory -v -count=1
```

Three shapes, over a heterogeneous 2-row `$rows` batch (one row WITH
`workload_id`, one row WITHOUT it — the exact omission shape
`cloudResourceServiceAnchorFields`/`azureCloudResourceNodeRow` produced before
this issue's fix):

1. **BASELINE** — the actual production shape, `SET n.workload_id =
   row.workload_id`, no coalesce, no default-fill.
2. **Option B** — `SET n.workload_id = coalesce(row.workload_id, "")`.
3. **Option A** — client-side default-fill (`{"uid": ..., "workload_id":
   ""}`), then the same bare `SET n.workload_id = row.workload_id`.

## Result

```
BASELINE (no coalesce, no default-fill): present-key row -> "wa"; missing-key row -> "row.workload_id"
RESULT: baseline missing-key row persisted as "row.workload_id" -- literal-corruption bug REPRODUCED on this backend build
OPTION B (coalesce): present-key row -> "wa"; missing-key row -> ""
OPTION A (default-fill): present-key row -> "wa"; filled-empty row -> ""
RESULT: coalesce() correctly nulls a missing map key -- Option B is viable
RESULT: default-fill produced the correct empty value -- Option A is viable
```

Two findings:

- The literal-corruption defect is confirmed still present on the currently
  pinned NornicDB build (`v1.1.11`, `eshu-nornicdb-pr261:149245885258`), not
  just the older `v1.1.9` #4995/#5450 proved it against. The BASELINE shape
  (identical to today's production Cypher) reproduces it live.
- **Both Option A and Option B are viable** on this backend build: `coalesce`
  now correctly resolves a genuinely-missing UNWIND row-map key to its default
  (unlike, e.g., the still-broken self-loop/cross-var identity comparison
  documented in `docs/public/reference/nornicdb-pitfalls.md`).

## Decision: Option A (writer default-fill)

Both being viable, Option A was chosen:

- It requires no change to `baseCloudResourceUpsertCypher` itself — lower
  diff risk against a Cypher template every existing
  `TestCloudResourceNodeWriter*` test already asserts the exact text of.
- It is unit-testable without a live backend
  (`TestCloudResourceNodeWriterDefaultFillsMissingRowKeys`,
  `TestCloudResourceRowKeyDefaultsCoversEverySetKey`), so the primary
  regression coverage does not depend on Docker/a live graph backend being
  available, while `TestCloudResourceNodeWriterLiveHeterogeneousBatchNeverPersistsLiteral`
  still proves it against the real backend.
- It matches the existing #4995/#5450 row-builder convention (explicit
  present-with-empty-default keys) instead of introducing a second parallel
  defaulting mechanism (`coalesce` in Cypher, plus builder-level explicit
  keys) that could drift out of sync with each other.

`go/internal/storage/cypher/cloud_resource_node_writer.go`'s
`cloudResourceRowKeyDefaults` map is the single authoritative list, kept next
to `baseCloudResourceUpsertCypher`, with a static lockstep test
(`TestCloudResourceRowKeyDefaultsCoversEverySetKey`) that fails the build if
the Cypher's `SET` clause and the defaults map drift apart.

## Data repair

No explicit backfill/retract pass is needed for a CloudResource whose source
facts are still emitted. `go/internal/reducer/README.md`'s
`DomainAWSCloudImageMaterialization` row states it is "[e]nqueued every
generation the scope carries `aws_resource` facts (the same persistent
trigger `DomainAWSResourceMaterialization` uses)" — i.e. `DomainAWSResourceMaterialization`
(and `DomainAzureResourceMaterialization`, the Azure sibling) re-run every
generation for any scope with current resource facts, not only on a delta.
`baseCloudResourceUpsertCypher` `MERGE`s on `uid` and then unconditionally
`SET`s every property (no `ON CREATE SET` gate), so the very next generation
that reprocesses a given resource overwrites every property fresh — including
any previously-corrupted literal — with the now-correct value. For any
CloudResource actively re-synced by its collector (the normal case: AWS/Azure
inventory collectors report each resource's full current state every sync,
not an incremental diff), this is self-healing via ordinary re-materialization
once this fix ships; no separate repair job is required.

For a resource that disappears from the cloud account entirely (no longer
observed by any sync), its CloudResource node stops being reprocessed and any
already-corrupted property would persist until whatever pre-existing
lifecycle mechanism reaps stale/orphaned nodes runs
(`go/internal/reducer/graph_orphan_sweep_runner.go`) — the same staleness
exposure any other property on that node already has independent of this
bug, and out of scope for #5714.

## `go/internal/query/cloud_resources.go`'s `"row.service_name"` mask

Kept. `cloudResourceRowFromGraph` strips a literal `"row.service_name"`
placeholder before it reaches the API. The writer is now fixed for future
writes, but a real deployment upgrading across this fix has already-persisted
nodes that only self-heal on their *next* materialization (see Data repair
above) — there is no guarantee that has happened for every node by the time
an operator upgrades and queries the API. The mask is a single string
comparison with no measurable cost; removing it would only reintroduce a
visible leak during that rollout window for zero benefit. It was not
broadened to the other 6 service-anchor keys or `running_image_ref`/
`running_image_digest`: there is no existing evidence (in this corpus or
otherwise) of those specific literals having reached a live API response, and
adding speculative masks for values that were never observed to leak is out
of scope for this fix.
