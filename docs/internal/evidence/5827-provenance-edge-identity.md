# #5827 provenance-edge identity and legacy repair evidence

Issue #5827 makes independent provenance assertions coexist when they use the
same relationship type and endpoints. The identity contract is now:

```text
(start node, end node, relationship type, scope_id, evidence_source)
```

This applies to `PUBLISHES` edges targeting `Package` and `PackageVersion`,
`BUILT_FROM`, and `DERIVED_FROM`.

## Root cause

Eshu's writers included `scope_id` and `evidence_source` in the relationship
property map, but the previous NornicDB default ignored relationship pattern
properties while evaluating `MERGE`. Two same-pair assertions therefore
collapsed into one edge. The later writer overwrote mutable properties, and a
retract by either owner could delete the shared row.

Splitting batches or serializing writers could not fix the behavior: the second
statement still matched the first relationship. Neo4j's relationship `MERGE`
contract includes the pattern properties in the match, so the backend behavior
was a compatibility defect.

Root-Cause Evidence: the live legacy-row reproduction began with one
endpoint-only relationship, then collapsed two owner-scoped assertions until
the backend matched `MERGE` pattern properties. The first full-corpus drain also
left one migration-flagged item recycling once per second after batch ACK; the
retained backlog drained after both production batch ACK paths atomically
cleared the flag.

## Backend prerequisite

The correction is orneryd/NornicDB#290 at exact source revision
`5d2731ae1b3328708f74f12c21658786abac641a`. Eshu's default Compose tag is
`eshu-nornicdb-pr290:5d2731ae1b33`, built from that full revision and labeled
with it. The Git build context uses the full 40-character commit fragment, so
older and newer Docker builders resolve the same immutable source without the
newer Git-context checksum query feature.

The backend change covers plain `MERGE`, generic and specialized `UNWIND`,
explicit transactions, deterministic concurrent create, numeric-width
equivalence, signed zero, and non-reflexive NaN semantics. Committed-write cache
invalidation occurs only after a successful authoritative transaction write.

### Backend performance proof

Same-host `GOMAXPROCS=1` benchmarks used the same source, corpus shape, storage
state, `-benchtime=20x`, and `-count=3`. Values below are the median nanoseconds
per operation.

| Path and fanout | Previous main | Corrected revision |
| --- | ---: | ---: |
| plain, 2 | 108,323 | 54,844 |
| plain, 32 | 150,975 | 50,492 |
| plain, 256 | 575,935 | 64,494 |
| UNWIND, 2 | 211,681 | 42,975 |
| UNWIND, 32 | 4,741,627 | 303,417 |
| UNWIND, 256 | 188,007,740 | 2,568,088 |

The corrected point lookup stays bounded as pair fanout grows. The compatibility
scan is reserved for legacy or collision recovery and is bounded to the endpoint
pair.

### Exact Eshu query trace

The retained harness at
`testdata/nornicdb/eshu_exact_provenance_trace_test.go` was placed in a clean,
detached NornicDB worktree at the pinned revision. It reads the four query
constants directly from Eshu's Go source with `go/parser`, then executes those
strings unchanged. The test creates the production endpoint constraints first:
`Repository.id`, `Package.uid`, `PackageVersion.uid`, and
`ContainerImage.digest`. It used `-tags nolocalllm` because the query executor
does not need the optional local-model archive.

```text
test "$(git rev-parse HEAD)" = 5d2731ae1b3328708f74f12c21658786abac641a
cp <eshu-worktree>/testdata/nornicdb/eshu_exact_provenance_trace_test.go \
  pkg/cypher/eshu_exact_provenance_trace_test.go
test "$(shasum -a 256 <eshu-worktree>/testdata/nornicdb/eshu_exact_provenance_trace_test.go | awk '{print $1}')" = \
  "$(shasum -a 256 pkg/cypher/eshu_exact_provenance_trace_test.go | awk '{print $1}')"
ESHU_ROOT=<eshu-worktree> go test -tags nolocalllm ./pkg/cypher \
  -run '^TestEshuExactProvenanceQueriesUseIndexedMergeHotPath$' \
  -count=1 -v
PASS
```

The retained output is
`docs/internal/evidence/5827-provenance-edge-hotpath-trace.txt`. It records the
pinned backend SHA, Eshu query-source commit and blob IDs, the harness digest
match, the parent test, all four named subtests, and `EXIT=0`; a zero-test pass
cannot satisfy that evidence.

Every exact query reported the same required trace:

| Eshu query constant | UnwindMergeChainBatch | MergeSchemaLookupUsed | MergeScanFallbackUsed | OuterScanFallbackUsed |
| --- | --- | --- | --- | --- |
| `canonicalProvenancePublishesPackageCypher` | true | true | false | false |
| `canonicalProvenancePublishesPackageVersionCypher` | true | true | false | false |
| `canonicalProvenanceBuiltFromCypher` | true | true | false | false |
| `canonicalProvenanceDerivedFromCypher` | true | true | false | false |

This covers every endpoint-key alternative used by the writer. The full-corpus
wall time therefore does not hide a generic node-scan fallback on these four
write shapes.

## Eshu writer proof

`TestProvenanceEdgeWriterLiveLegacyRowSetMigration` creates the exact legacy
state first: one endpoint-only relationship. It then replays two independent
assertions in both orders for all four writer shapes:

- `PUBLISHES` to `Package`
- `PUBLISHES` to `PackageVersion`
- `BUILT_FROM`
- `DERIVED_FROM`

Every case starts at one relationship and finishes at exactly two, preserving
both `scope_id` and `evidence_source` values.

`TestProvenanceEdgeWriterLiveSamePairAssertionIsolation` repeats each shape five
times, replays duplicates, runs eight concurrent writers, and retracts one
assertion. It requires exactly three relationships before retract and exactly
the other two owners' relationships afterward. This proves idempotency,
convergence, and retract isolation without reducing worker count or serializing
the write path.

The live tests ran through Eshu's Bolt driver against an isolated fresh
NornicDB container built from the exact revision above:

```text
go test ./internal/storage/cypher \
  -run 'TestProvenanceEdgeWriterLive(LegacyRowSetMigration|SamePairAssertionIsolation)' \
  -count=1 -v
PASS
```

## Legacy-row repair

Migration 096 records a one-time marker named
`provenance_edge_identity_upgrade_096` and attaches a durable
`provenance_edge_identity_upgrade_required` capability flag to current
replayable work for the affected `package_source_correlation` and
`container_image_identity` domains. It reopens current successful work, leaves
pending/retrying work in place, and marks claimed/running work for replay. A
domain-filtered insert trigger covers work created by an old pod after the
pre-upgrade hook; an update trigger covers later operator reopens, but skips
already-flagged claim/retry transitions.

An old reducer cannot clear the capability flag. Its successful ACK or terminal
failure is therefore bounced back to pending by a terminal-transition trigger.
The new reducer clears the flag atomically on both single-item and batch ACKs,
or on a genuine dead letter; retries retain it. This closes the post-hook
old-worker race without reducing concurrency or relying on a one-time marker
alone.

The repair deliberately uses the existing retract-before-write path. It does
not mutate graph rows in SQL, invent graph endpoints, or bypass reducer truth.
The replay removes each legacy owner-scoped row and rematerializes it with the
new identity.

The live Postgres proof applies the real migration to current succeeded,
pending, retrying, running, stale-generation, and unrelated-domain fixtures. It
also inserts affected work after the hook. The proof drives old-binary ACK and
dead-letter SQL, then the production new-binary `ReducerQueue.AckBatch` paths
for both affected domains and `ReducerQueue.Fail`. It verifies old terminal
transitions replay, new terminal transitions clear the capability flag,
unrelated/stale work remains untouched, and reapplying the marker is
idempotent. The batch regression was written red after the first full-corpus
run exposed a missing flag clear: one affected item recycled once per second
until the drain timed out. The same retained backlog drained after the batch
ACK correction.

```text
go test ./internal/storage/postgres \
  -run TestProvenanceEdgeIdentityUpgradeSeedsCurrentReplayOnceLive \
  -count=1 -v
PASS
```

The permanent trigger cost is measured on the same live Postgres schema with
seven alternating before/after samples of the same 500-item affected-domain
enqueue, claim, and ACK batch. Trigger-level `WHEN` predicates keep already
flagged claim/retry transitions out of PL/pgSQL. The final median was 27.299 ms
without the triggers and 29.051 ms with them: 1.753 ms per batch, or 3.505
microseconds per item.

Performance Evidence: on live Postgres using migration 096 and the same
500-item affected-domain enqueue, claim, and ACK input, the seven-sample median
was 27.299 ms before and 29.051 ms after, a bounded 3.505 microseconds per item.
The exact NornicDB #290 source build completed the 30-repository B-7 run in 132
seconds with zero residual queue items, required failures, or advisory warnings.
The #5828 counter correction adds no graph query, loop, or batch allocation: the
same single counter add moves from before each writer call to its successful
return and changes only the bounded outcome label. Successful nonempty writes
therefore keep the same operation count, while failed writes do less work.

```text
go test ./internal/storage/postgres \
  -run 'TestProvenanceEdgeIdentityUpgrade(SeedsCurrentReplayOnce|TriggerNoRegression)Live' \
  -count=1 -v
PASS
```

## Golden truth

The B-12 snapshot requires at least two `PUBLISHES` relationships. rc-164
filters `PACKAGE_PUBLICATION_CORRELATION`, while rc-172 filters
`PACKAGE_OWNERSHIP_CORRELATION` for the same Repository and PackageVersion
endpoints. One surviving edge cannot satisfy both assertions.

`BUILT_FROM` remains narrowed by its canonical OCI evidence kind and
`source_tool=oci`. Package assertions use the canonical explicit
`source_tool=unknown` fallback because their decisions do not carry a truthful
ecosystem token.

The exact-source full-corpus gate materialized 25 `BUILT_FROM` assertions after
scope identity stopped collapsing them. The snapshot ceiling is 40: bounded
above the observed result for fixture growth while still catching an accidental
fanout explosion.

```text
scripts/verify-golden-corpus-gate.sh
528 pass, 0 required-fail, 0 advisory-warn
elapsed 132s; budget ceiling 1800s
```

The measured phases were bootstrap 3 seconds, collect 16 seconds, first drain
66 seconds, maintenance drains 21 seconds, and graph/query checks 2 seconds.
Every graph, API, MCP, drain, timing, and wall-time assertion passed.

## Rollout and rollback

Roll out the exact NornicDB #290 source or a released successor containing the
fix before deploying the Eshu writer and migration. The old backend would still
collapse the new identity properties.

The Helm chart fails closed when `nornicdb.enabled=true` unless the operator has
replaced the bundled v1.1.11 image with an immutable verified build and set
`nornicdb.capabilities.relationshipMergePropertyIdentity=true`. The capability
flag is an explicit operator acknowledgement, not automatic version inference.

Migration 096 is a forward-only compatibility fence. After it is applied, an
old reducer cannot drain the affected domains, so returning only the writer or
backend to an older build is not a valid rollback. Stop every affected reducer
before changing either component. Prefer rolling forward with the fixed backend
and writer. A full rollback requires restoring both Postgres and graph backups
taken before migration 096, deploying the old Eshu and backend versions, and
only then resuming reducers. Do not disable or drop the fence triggers against
an upgraded graph: the durable marker prevents reseeding and would leave work
created during the rollback interval without a trustworthy repair path.

## Observability impact

The queue snapshot now exposes the migration marker and current replay-required
count as `provenance_edge_identity_upgrade_applied` and
`provenance_edge_identity_upgrade_required` in `/admin/status` JSON. Text status
renders the same values on a dedicated upgrade line. `/metrics` exports the
bounded gauges `eshu_runtime_provenance_edge_identity_upgrade_applied` and
`eshu_runtime_provenance_edge_identity_upgrade_required`, labeled only by
service name. A nonzero required count after an old reducer reports successful
ACKs attributes the drain loop to migration 096 rather than graph latency or
ordinary retry pressure.

Observability Evidence: the live status proof reported the migration as applied
with seven replay-required items before terminal transitions and one deliberately
retained item afterward. Operators can observe those same bounded counts in
JSON, text status, and the two service-labeled gauges without per-item or
high-cardinality identifiers.

`eshu_dp_provenance_edges_total{outcome="submitted"}` now measures rows accepted
by each successful provenance writer call across all four writer shapes. Rows
from a failed call emit no point; retract errors, empty projections, and unwired
writers emit none. A missing endpoint remains a submitted writer no-op, and a
successful retry can count the same identity again, so this event counter is
not a unique durable-edge gauge. The status query adds one aggregate filter
over the already-scanned active queue snapshot and one indexed marker lookup;
it adds no queue mutation, per-item labels, spans, or logs.
