# Oversized Index Key Guard Evidence

Issue #7058: in a 984-repository Neo4j trial (commit 8d5949f90) a 32,473-byte
`Module.name` produced by the parser bug in #7056 hit the `module_name_lookup`
range index. Neo4j rejected it with
`Neo.DatabaseError.Statement.ExecutionFailed: Property value is too large to index`,
and because the canonical write is one atomic transaction per repository, none
of that repository's graph was written. NornicDB accepts such values silently.

Round 1 added `canonical.DropOversizedIndexKeys`, called by
`CanonicalNodeWriter.Write` before any statement is built. It removes Module
rows whose name, entity rows whose name plus path, and Parameter rows whose
name plus path exceed `canonical.MaxIndexedKeyBytes` (8000 UTF-8 bytes), plus
IMPORTS, HAS_PARAMETER and CONTAINS rows that reference those keys. The node is
absent rather than written with a truncated or hashed identity. Rows at or
under the bound are unchanged, and when nothing is dropped the materialization
is returned with its original slices.

## Limit measurement

Measured by binary search against `neo4j:2026-community@sha256:eabfbb04…`
(Neo4j Kernel 2026.08.1, `range-1.0`), one CREATE per probe:

| Key shape | Largest accepted string bytes |
| --- | --- |
| `(:Module {name})`, ASCII | 8164 |
| same, 2-, 3-, 4-byte runes | 8164, 8163, 8164 |
| `(:Function) (name, path, line_number)` unique | 8151 total for name + path |

The limit counts encoded bytes, not characters. The source cap is
`DynamicSizeUtil.FIXED_MAX_KEY_VALUE_SIZE_CAP = 8175`, enforced by
`GenericIndexKeyValidator` at commit (neo4j `2026.06` source). 8000 leaves at
least 151 bytes for extra key slots such as `kind` on `K8sResource` and
`CrossplaneClaim`.

Root-Cause Evidence: `TestLiveCanonicalWriteSkipsOversizedIndexKeys` with the
`dropOversizedIndexKeys` call removed from `Write` fails on that Neo4j image with
`canonical atomic write: Neo4jError: Neo.DatabaseError.Statement.ExecutionFailed (Property value is too large to index ... Index( id=11, name='module_name_lookup', type='RANGE' ...`,
the trial error. With the guard it passes: repository, file, normal Function,
normal Module and its IMPORTS edge are written; the 32,473-byte Module and the
8,053-byte Function are absent; an 8,000-byte Module name and an 8,000-byte
Function (name + path) key are both written.

No-Regression Evidence: no Cypher text, batching, transaction shape, or
statement order changes. The guard is one pass of `len()` checks over the row
slices. `BenchmarkDropOversizedIndexKeysNoneDropped` (50,000 entities, 5,000
modules, 20,000 imports, 20,000 parameters, 15,000 class/nested rows, nothing
dropped) runs in about 365 µs with 0 allocations on an Apple M5 Max
(`go test ./internal/projector/canonical -bench DropOversizedIndexKeys -count=3`:
367049, 364861, 364731 ns/op). A canonical write of a repository that size
takes seconds, so the added cost is well under 0.1%.
`TestDropOversizedIndexKeysLeavesNormalMaterializationUntouched` pins that the
common path returns identical rows and the same backing arrays.
`go test ./internal/storage/cypher/... ./internal/projector/... ./internal/telemetry/... -count=1`
passes.

Observability Evidence (round 1): counter
`eshu_dp_graph_oversized_index_keys_skipped_total` labeled `node_label`
(closed canonical label set) and `property` (`name` or `path`), plus one WARN
per skipped node, `canonical row skipped: indexed value exceeds key size limit`,
carrying `scope_id`, `repo_id`, `generation_id`, `node_label`, `property`,
`key_bytes`, `limit_bytes`, `entity_id`, `file_path` and a 64-byte
`value_prefix`. The canonical write span gets `oversized_index_keys_skipped`.
Rows are in `docs/public/observability/telemetry-coverage.md`,
`docs/public/reference/telemetry/metrics.md` and
`docs/public/reference/telemetry/metrics-reducer-storage.md`.

## Round 2: schema-derived guard on every graph write

Review of round 1 found three gaps. The reducer's semantic-entity writer (legacy
rows on Neo4j) MERGEs Function, Annotation, Variable, semantic Module and other
labels from facts, outside the canonical guard, so the node came back one stage
later and a value over 8151 bytes dead-lettered the semantic stage (F1).
Terraform state resources and modules and the `kind` slot of the `K8sResource`
and `CrossplaneClaim` keys rode the same atomic write unguarded (F2). Single
property indexes on entity metadata were never audited (F3).

Root cause: the guard was a per-writer list of hand-picked properties. Round 2
derives the guard from the schema and applies it at the one seam every graph
write passes through:

- `graph.SchemaIndexKeys` parses every non-fulltext `CREATE INDEX` and
  `CREATE CONSTRAINT` the schema can create on either backend (raw composite
  constraints included) into label → indexed properties, and errors on any
  shape it cannot read.
- `graph.GuardIndexKeyWrites(cypher, params)` reads the statement's write
  shape (MERGE/CREATE inline maps, `SET n.p = expr`, `SET n += row.map`,
  `UNWIND $rows AS row`), sums the string bytes each row puts into every schema
  key of the written labels, and drops rows over `MaxIndexKeyBytes` (8000).
  A dropped row takes every edge the same row writes; edges in other statements
  MATCH the node and cannot attach. A value in a scalar parameter skips the
  statement.
- `storage/cypher.InstrumentedExecutor` runs it on every `Execute` and
  `ExecuteGroup`. Every production write chain (ingester, projector,
  bootstrap-index, reducer canonical, semantic, edge and node writers) is built
  on it; the reducer's workload/platform materializer chain has none, so
  `reducerCypherExecutor` calls the same guard. The canonical materialization
  guard stays: it drops name-keyed dependent rows (IMPORTS, HAS_PARAMETER,
  class/nested CONTAINS) that do not share a statement with the node.

Coverage guard: `TestSchemaIndexKeysCoverEveryIndexStatement` fails when a DDL
statement is missing from the key map, and
`TestGuardIndexKeyWritesCoversEverySchemaKey` drives one oversized write per
schema key (all labels, all composite keys) and fails if the guard does not
drop it.

Writers covered (every `MERGE`/`CREATE`/`SET` on a node label reaches the
backend through `InstrumentedExecutor` or `reducerCypherExecutor`):
canonical node writer (code entities, files, directories, modules, parameters,
Terraform state resources/modules/outputs, OCI and package-registry nodes),
semantic entity writer (all five write modes), code taint and interproc
evidence, cloud resource, EC2 instance/identity/posture/exposure, RDS posture,
S3 exposure and external-principal grant, security-group CIDR, KubernetesNamespace,
KubernetesWorkload, secrets/IAM graph, incident routing evidence, package
registry artifact/event, shell-exec and shared edge writers, workload and
platform materializers. Not covered: `graph.BatchMerge*` helpers (no production
caller), orphan sweep and retract statements (deletes only), fulltext indexes
(tokenized; Lucene's 32,766-byte term limit is a separate, untested bound).

Root-Cause Evidence (round 2, neo4j@sha256:eabfbb04…, 2026.08.1):
`TestLiveCanonicalWriteSkipsOversizedIndexKeys` now runs through
`InstrumentedExecutor` with 9000-byte values (above 8164 and 8151), an
8100-byte Function key that Neo4j would accept but the guard skips on every
backend, Terraform state resources and modules, the reducer's Neo4j semantic
writer (legacy rows), and a second generation. With the statement guard
disabled it fails on the canonical write with `Property value is too large to
index ... name='tf_state_resource_name'`; with the Terraform rows removed it
fails on the semantic stage with `... name='annotation_unique'`. With the guard
it passes (4.54 s on a fresh container including schema creation, 0.23 s on a warm rerun): all normal nodes and edges land in both generations and no
oversized or mid-range value does.

No-Regression Evidence (round 2, Apple M5 Max, host load average 2.8–5.0):
`go test ./internal/graph -bench GuardIndexKeyWrites -benchmem -count=5`:
500-row semantic Function batch 71.9–72.7 µs, 500-row canonical entity batch
(`SET n += row.props`) 113.0–117.1 µs, 0 allocs/op in steady state. The
canonical materialization guard reran at 358.6–367.2 µs, 0 allocs
(`BenchmarkDropOversizedIndexKeysNoneDropped`, 5 runs), in line with round 1's
365 µs. No Cypher text, batching, transaction shape or statement order changes;
rows are filtered in place and the caller's parameters are never mutated.

Observability Evidence (round 2): the counter is now
`eshu_dp_graph_oversized_index_keys_skipped_total` (renamed from the unreleased
`canonical_` name) with `node_label` and `property` taken from the schema
(closed sets). The statement guard logs one WARN per dropped row,
`graph write skipped: indexed value exceeds key size limit`, with `operation`,
`node_label`, `property`, `key_bytes`, `limit_bytes`, `param`, `scope_id`,
`repo_id`, `generation_id`, `entity_id`, `file_path`, `value_prefix` and
`statement_skipped`, and sets `oversized_index_keys_skipped` on the current
span. The counter counts once per write attempt: the guard sits above the
transient-retry executor, so driver retries do not recount; a work-item retry
that rebuilds the write counts the same node again.
