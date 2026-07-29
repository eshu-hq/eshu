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

**Removal trigger** (named per hostile-review P2 finding — a mask with no
stated removal condition would silently keep absorbing a regression of this
exact literal at the API layer, hiding the operator-visible symptom that
would otherwise surface a `service_name` regression faster than any other
signal): safe to delete once every AWS/Azure scope has re-materialized at
least once post-deploy — the normal case within one collector sync cycle,
since `DomainAWSResourceMaterialization`/`DomainAzureResourceMaterialization`
re-run every generation a scope carries current resource facts (see Data
repair above). Do not remove opportunistically on a fixed calendar date;
confirm the re-sync has actually happened (e.g. no CloudResource node with
`service_name = "row.service_name"` in a live query) before deleting. The
same trigger and reasoning is noted at the mask itself
(`go/internal/query/cloud_resources.go`, `cloudResourceRowFromGraph`'s doc
comment).

## CI enforcement decision (#5714 acceptance bullet 3, hostile-review P1)

The issue's third acceptance bullet asks to "consider a shared helper or a
golden-gate `MaximumNodePropertyCount`-style guard so a future over-broad/
literal write fails loud." Two ways to get CI-enforced coverage were
considered:

- **(a) Wire the existing live regression test into the live conformance
  lane.** `TestCloudResourceNodeWriterLiveHeterogeneousBatchNeverPersistsLiteral`
  (`go/internal/storage/cypher/cloud_resource_node_writer_heterogeneous_batch_live_test.go`)
  already proves the defect class against a real NornicDB backend, but its
  gate env var (`ESHU_CLOUDRESOURCE_NODE_WRITER_LIVE`) was set nowhere outside
  the test file itself, so it always skipped — including in CI. Nothing was
  actually exercising this regression class end to end.
- **(b) Add a new B-7/B-12 golden-corpus fixture** mirroring
  `rn-cloud-resource-running-image` (#5450's `MaximumNodePropertyCount`
  ceiling guard on the same writer and label): a new heterogeneous AWS or
  Azure cassette resource plus a snapshot entry pinning
  `required_node_properties`/`maximum_node_property_count` for the 7
  anchor keys.

**Chose (a).** The AWS/Azure cassettes carry zero anchor-bearing
`CloudResource` resources today (confirmed by `rg` over
`testdata/cassettes/awscloud/` and `testdata/cassettes/azurecloud/` — no
`service_name`/`workload_id` hits), so (b) would require introducing a new
fixture resource specifically to make a floor assertion non-vacuous, which
moves the total AWS/Azure `CloudResource` node count and every count-derived
assertion downstream of it (the golden-corpus gate is a live Docker gate this
executor is not permitted to run and the orchestrator serializes) — a
materially larger, corpus-perturbing change for a guard whose job is only to
catch a regression in a writer this PR already covers at the unit level
(`TestCloudResourceNodeWriterDefaultFillsMissingRowKeys`,
`TestCloudResourceRowKeyDefaultsCoversEverySetKey`) and, with (a), at the live
level in every CI run of `.github/workflows/e2e-tests.yml`'s backend-matrix
job. (a) is strictly cheaper and does not touch the corpus.

Implemented in `scripts/verify_backend_conformance_live.sh`'s existing
NornicDB-only allowlist block (the same pattern
`TestLiveNornicDBRetryConflictClassificationContract`/
`TestTerraformResourceWriterLiveClearsStaleAttributeOnRefresh` already use):
sets `ESHU_CLOUDRESOURCE_NODE_WRITER_LIVE=1` and runs
`TestCloudResourceNodeWriterLiveHeterogeneousBatchNeverPersistsLiteral` as its
own `go test` invocation. That script is wired into
`.github/workflows/e2e-tests.yml`'s "Run live backend conformance" step,
which runs on every push to `main` and on every PR touching `go/**` (path
filter), across the `nornicdb`/`neo4j` backend matrix — so this regression
class now fails loud in CI, not just locally. Verified locally end to end
against a bare (non-Compose) NornicDB container on private ports: the full
`scripts/verify_backend_conformance_live.sh` run, including the new block,
passed (`ok  go/internal/storage/cypher  1.111s` for the new test).

(b) remains available as a follow-up if a future maintainer wants the
non-vacuous corpus-level floor+ceiling assertion in addition to (a); it was
not rejected as wrong, only as disproportionate to land in this change.

## P3 — informational, no action taken (out of #5714's scope)

- **`canonicalEC2InstanceUpsertCypher`** (`go/internal/storage/cypher/
  ec2_instance_node_writer.go`) is a genuinely separate Cypher constant (not
  `canonicalCloudResourceUpsertCypher`) that shares the same `UNWIND $rows AS
  row ... MERGE (r:CloudResource {uid: row.uid}) SET ...` shape and ~9 of the
  same base keys (`arn`, `resource_id`, `resource_type`, `name`, `state`,
  `account_id`, `region`, `service_kind`, `correlation_anchors`) plus its own
  ten EC2-posture-specific keys, with no default-fill backstop of its own.
  It is outside #5714's "feeding `canonicalCloudResourceUpsertCypher`" scope
  by the issue's own text, so it is not fixed here and no new issue is filed
  for it per this session's standing instruction against issue sprawl; flagged
  for the package owner to decide whether the same backstop pattern should
  extend there.
- **The `ifadeterminismteeth` build-tag SET clause**
  (`cloud_resource_node_writer_teeth.go`'s `teethCloudResourceUpsertExtraSet`,
  `r.ifa_teeth_seq`/`r.ifa_teeth_write_order`) is outside
  `TestCloudResourceRowKeyDefaultsCoversEverySetKey`'s lockstep scan surface
  (the test scans `baseCloudResourceUpsertCypher`, not the
  `ifadeterminismteeth`-only concatenation). This is intentional and benign:
  that build tag never links in a normal/CI/production build
  (`cloud_resource_node_writer_teeth_off.go` supplies the empty string
  instead, proven by `TestCanonicalCloudResourceUpsertCypherExcludesTeethClauseByDefault`),
  and `ifaTeethStampCloudResourceRow` (the row-side counterpart) always stamps
  both keys unconditionally when that tag IS active, so the two teeth keys are
  never actually omitted from a row in practice — unreachable, not merely
  unchecked.
