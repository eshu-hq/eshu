# NornicDB Behavior and Pitfalls Reference

This page is the operational companion to
[NornicDB Tuning](nornicdb-tuning.md). It records NornicDB behaviors that have
affected Eshu integration and proof work.

Use it to avoid rediscovering the same failure shape. Still check the current
NornicDB source before patching.

## How To Use This Page

1. Read the matching section before patching NornicDB or routing around a
   suspected NornicDB bug.
2. Validate the behavior against the current `NornicDB-New` checkout that built
   the image under test.
3. Check upstream docs and release notes for the pinned `NORNICDB_IMAGE`.
4. If the current reproduction differs, update this page with the reproduction,
   observed shape, and either the root cause or open question.

NornicDB changes quickly. A documented behavior may already be fixed in the
binary you are testing.

## Pitfall: Recreating Single-Property `UNIQUE` Constraints On A Live Store

### Observed shape

On a running NornicDB instance with existing nodes:

1. `DROP CONSTRAINT <name>` succeeds.
2. `CREATE CONSTRAINT <name> FOR (n:Label) REQUIRE n.prop IS UNIQUE` succeeds.
3. A later write that matches an existing node can fail commit with a uniqueness
   violation against the matched node itself.

The row remains readable. `MATCH (n {prop: value}) RETURN id(n)` still finds it.

### Hypothesis

The value-cache rebuild can register existing values with one node ID shape
while transactional validation compares another. The commit path then treats the
matched node as another node with the same unique value.

Verify this against the current `NornicDB-New` source before relying on the
hypothesis.

### Eshu implications

- Do not use drop/create constraint cycles as a live-stack debug experiment.
  Tear down the dedicated graph volume and start fresh.
- Do not change Eshu schema bootstrap to rerun `CREATE CONSTRAINT` after graph
  writes. Schema DDL belongs before writes.
- If a read/update of an existing node fails with a false `UNIQUE` violation,
  check this pitfall before changing writer logic.

### Validation

Use an isolated Compose project: run data-plane schema bootstrap, write one
node for a label with a uid-style unique constraint, drop and recreate that
constraint through the Bolt HTTP endpoint, then reissue a `MATCH ... SET`
against the same node. Tear the stack down after the experiment.

## Pitfall: Concurrent `MERGE` Can Lose At Commit-Time `UNIQUE`

### Observed shape

Two concurrent writers can run the same canonical `MERGE` for a uid. Both may
plan a create, one commits, and the other loses at commit with a uniqueness
violation such as:

```text
Neo4jError: Neo.ClientError.Transaction.TransactionCommitFailed
(commit failed: constraint violation:
 Constraint violation (UNIQUE on TerraformResource.[uid]):
 Node with uid=<X> already exists (nodeID: <Y>))
```

That is normal concurrent `MERGE` behavior. Re-executing the same MERGE after
the winning commit should match the existing node.

### Eshu status

Eshu handles this in `go/internal/storage/cypher/retrying_executor.go`.
`RetryingExecutor.ExecuteGroup` retries commit-time unique conflicts when every
statement in the group is MERGE-shaped. Mixed groups are not retried because
re-executing non-MERGE statements after partial success can be unsafe.

The retry classifier uses the typed Neo4j error code
`Neo.ClientError.Transaction.TransactionCommitFailed` or
`Neo.TransientError.Transaction.Outdated` when the driver exposes one, then
validates the unique-conflict body. Untyped or wrapped errors keep the
historical fallback for `failed to commit implicit transaction` and
`commit failed: constraint violation` shapes.

No-Regression Evidence: `go test ./internal/storage/cypher -run
'TestRetryingExecutor(ClassifiesTypedNornicDBTransactionCommitFailedByCode|RetriesNornicDBMergeUniqueConflict|RetriesNornicDBMergeUniqueConflictV1045Format|ExecuteGroupRetriesOnCommitTimeUniqueConflict|ExecuteGroupDoesNotRetryNonMergeStatements)'
-count=1` proves typed error-code classification, historical substring
fallbacks, MERGE-only group retry, and mixed-group non-retry behavior.
`scripts/verify_backend_conformance_live.sh` now runs
`TestLiveNornicDBRetryConflictClassificationContract` only in the NornicDB live
backend lane, where the pinned service must still surface a retry-classifiable
commit-time UNIQUE conflict.

Observability Evidence: the retry loop keeps the existing
`eshu_dp_neo4j_deadlock_retries_total` counter and adds its bounded `reason`
label (`connectivity_error`, `transient_error`, `write_conflict`, or
`commit_unique_conflict`) alongside `write_phase`. The metric never carries a
raw error, repository id, node id, or statement. The retry warning log,
max-retry behavior, queue-visible retryable error type, span names, status
fields, worker knobs, and queue contract remain unchanged.

### Eshu implications

Do not serialize workers to hide this race, and do not add preflight `MATCH`
checks as the fix for canonical MERGE re-projection. Route canonical projection
through the retrying phase-group executor. If the error reappears, verify
`retryable_error_test.go` and `retrying_executor_test.go` before changing queue
or worker knobs.

For package-registry identity specifically, Eshu also coordinates package UID
writes with Postgres transaction-scoped advisory locks in the projector runtime.
That lock narrows cross-process overlap for `Package.uid` without reducing
global worker counts; the retrying executor still remains the backend safety
net for other MERGE-shaped races and changed NornicDB error wrapping.

When the in-loop retry budget is exhausted, or a transient
`*TransactionExecutionLimit`/`*ConnectivityError` escapes a canonical write,
`CanonicalNodeWriter.Write` wraps the error with `WrapRetryableNeo4jError` so the
projector queue classifies it `projection_retryable` and requeues it with
backpressure (`retryDelay`, then bounded by `maxAttempts`) instead of recording a
terminal `projection_failed` dead letter. A genuinely terminal error such as a
schema constraint violation is not wrapped and stays terminal. If canonical
projection still dead-letters on a transient NornicDB write conflict, verify that
the escaping error implements `Retryable()` before lowering worker or batch
knobs; serializing writers is not the fix.

## Pitfall: Composite `IS UNIQUE` Constraints Are Not The NornicDB Contract

### Observed shape

NornicDB rejects Neo4j's composite uniqueness syntax such as:

```cypher
CREATE CONSTRAINT function_unique IF NOT EXISTS
FOR (f:Function) REQUIRE (f.name, f.path, f.line_number) IS UNIQUE
```

Eshu's NornicDB schema dialect deliberately omits those statements and creates
`uid` uniqueness constraints plus lookup indexes for the same labels.

### Eshu implications

Do not assume NornicDB will reject duplicate `(name, path, line_number)` tuples
directly. The parity contract is app-layer identity derivation before graph
write: canonical source-local projection derives `uid` from repo, relative
path, entity type, entity name, and start line for labels such as `Function`
and `Class`, then the NornicDB `uid` constraint makes duplicates impossible.

Do not fix duplicate code identities with worker serialization or preflight
graph reads. If duplicates appear, first verify projector canonical UID
derivation and schema bootstrap `uid` constraints/indexes.

### Validation

Run the projector identity regression and graph schema dialect tests:

```bash
cd go
go test ./internal/projector -run TestBuildCanonicalMaterializationCanonicalizesDuplicateCodeEntityIdentity -count=1
go test ./internal/graph -run 'TestSchemaStatementsForBackend(CoversNornicDBCompositeIdentityWithUID|PreservesNeo4jCompositeUniqueness)' -count=1
```

No-Regression Evidence: the #2265 fix keeps Neo4j's direct composite
constraints, keeps NornicDB's composite constraint suppression, and makes the
source-local projector derive canonical `uid` values for name/path/line entity
labels before canonical graph writes. `go test ./internal/projector
./internal/graph ./internal/storage/cypher ./internal/backendconformance
-count=1` covers duplicate Function/Class identity convergence, graph schema
dialect output, canonical entity write shape, and the backend-conformance spec.

No-Observability-Change: no runtime metric, span, log field, queue stage,
worker knob, schema bootstrap phase, or status field changes. Existing
canonical write spans, phase logs, graph query spans, and query-duration
metrics continue to expose graph write failures and retries.

## Pitfall: `CREATE INDEX IF NOT EXISTS` Rebackfills Existing Property Indexes

### Observed shape

In pinned NornicDB v1.1.11, `IF NOT EXISTS` is accepted syntax but is not proof
that reapplying property-index DDL is a no-op. The
[`executeCreateIndex`](https://github.com/orneryd/NornicDB/blob/v1.1.11/pkg/cypher/schema.go#L597-L646)
path calls the index backfill after `AddPropertyIndex` returns, including when
the property index already exists. The
[`PropertyIndexInsert`](https://github.com/orneryd/NornicDB/blob/v1.1.11/pkg/storage/schema.go#L1874-L1898)
path appends node IDs without an observable duplicate guard.

Performance Evidence: an identical property-index statement reissued against
the retained 887-repository graph took 15.345136 seconds. Unchanged graph node
and edge counts did not prove the internal index was unchanged, so that
candidate was removed and rejected rather than shipped.

### Eshu implications

Do not repeat experimental index DDL against a retained evidence stack. Prove
the candidate on an isolated populated store first:

1. Measure the first create and record the index-backed result set.
2. Reissue the identical statement and compare duration plus index-backed
   result and index-entry cardinality where the backend exposes it.
3. Prove ordered query exactness and bidirectional result diff `0/0`.
4. Restart, rerun Eshu schema bootstrap, and verify the same query readback.
5. Prove rollback or cleanup, then destroy the isolated volume.

Eshu's Postgres graph-schema fingerprint normally skips an already-applied
schema application. That is defense in depth for the normal bootstrap path; it
does not prove the backend DDL itself is idempotent.

No-Observability-Change: this documents a validation requirement. It changes
no runtime schema statement, metric, span, log, queue, or worker behavior.

## Pitfall: Persisted Graph Store Fails To Reopen After Dictionary Corruption

### Observed shape

A NornicDB-backed Eshu graph store can fail before Bolt or HTTP readiness with:

```text
failed to load persisted schema: schema: rebuild unique values:
decode node: property key id <id> not in dictionary for namespace "nornic"
```

When this happens, API and MCP graph-backed reads cannot recover until the graph
backend opens or the graph volume is rebuilt.

### Eshu recovery contract

For Eshu, NornicDB graph data is rebuildable projection state. Source systems,
repository snapshots, collector facts, workflow state, content, and Postgres
queues are the durable inputs.

Supported response:

1. Preserve the broken graph volume or logs when forensic evidence matters.
2. Recreate only the NornicDB data directory or PVC.
3. Run data-plane schema bootstrap before graph writes resume.
4. Replay projection work from stored facts or recollect from source systems.
5. Verify API/MCP health and queue-zero with `GET /api/v0/index-status`.

Do not delete Postgres unless the accepted recovery plan is full source
recollection. Do not make Eshu silently delete graph data at startup.

## Pitfall: Node-Label Disjunction In A `MATCH` Matches Zero Rows

### Observed shape

Two related matching quirks make an all-source-labels retract unreliable.
Measured on the canonical `timothyswt/nornicdb-cpu-bge:v1.1.11` (and, for the
disjunction, `v1.1.9`):

```cypher
-- 1. Node-label disjunction matches zero rows (v1.1.9 and v1.1.11):
MATCH (s:Function)-[r:CALLS]->() RETURN count(r)         // 1
MATCH (s:Function|Class)-[r:CALLS]->() RETURN count(r)   // 0  (broken)
MATCH (s:Function|Class {uid: $u}) RETURN count(s)       // 0  (broken, even with a property key)

-- 2. Unlabeled source scan is unreliable on v1.1.11 (regression from v1.1.9):
MATCH (s)-[r:CALLS|REFERENCES|INSTANTIATES]->() WHERE s.repo_id IN $ids RETURN count(r)
--   drops some source labels (e.g. a File-sourced REFERENCES), inconsistently
--   by internal label-iteration state. On v1.1.9 the same query is complete.
```

Relationship-type disjunction (`-[r:CALLS|REFERENCES]->`) works on both. Only the
source **node** anchor is affected. A third quirk compounds it: on v1.1.11
multiple `DELETE` statements sharing a single managed Bolt transaction do not all
apply — a grouped per-label retract leaves some edges behind, while the same
statements run as separate auto-commit transactions delete every edge.

### Eshu implications

A retract that anchors its source on a label disjunction, or on an unlabeled
`(source)` scan, silently under-deletes on NornicDB. Issue #5116 was exactly
this: the code-call edge retract matched
`(source:Function|Class|Struct|Interface|TypeAlias|File)`, so it deleted zero
edges and stale `CALLS`/`REFERENCES`/`INSTANTIATES` edges survived every
reprojection.

The only shape that reliably retracts every source on both pinned versions is a
**single label per statement**, so the fix fans the retract out to one statement
per source label (`MATCH (source:Function)-[rel:CALLS|REFERENCES|INSTANTIATES]->()
WHERE source.repo_id IN $repo_ids AND rel.evidence_source = $evidence_source
DELETE rel`, repeated for `Class`/`Struct`/`Interface`/`TypeAlias`/`File`). The
statements run **sequentially** (each in its own transaction), not grouped,
because of the managed-transaction quirk above. Each statement is independently
scoped and idempotent, so sequential execution is safe. See
`buildCodeCallRetractStatements` and `executeCodeCallRetractStatements` in
`go/internal/storage/cypher`.

Do not resolve this by dropping the label to an unlabeled `(source)` scan (it
passes on v1.1.9 but silently under-deletes on v1.1.11), and do not group the
per-label statements into one transaction. A sibling instance of the same
anti-pattern is still open and tracked in #5116: the write-path fallback
templates (`batchCanonicalCodeCallUpsertCypher` and friends) silently write
nothing for unresolved-label endpoints. The inheritance retract carried the
same node-label disjunction and was fixed the same way in #4367
(`buildInheritanceRetractStatements`). The SQL-relationship retract carried
both remaining shapes: its per-label statements ran grouped through one
managed transaction (measured on v1.1.11: the first DELETE never applied,
deterministically across runs — sequenced in #5128, live proof
`TestReducerSQLRelationshipRetractGraphTruth`), and its non-GroupExecutor
fallback was an unlabeled `(source)` scan, removed in #4367 when the retract
moved to one statement per write-capable source label
(`buildSQLRelationshipRetractStatements`). The rationale EXPLAINS delta
retract carried the disjunction on its TARGET node
(`...->(target:Function|Class|...|File) WHERE target.path IN ...`) — probed on
v1.1.11: it deletes nothing — and was fixed the same way in #4367
(`BuildRetractRationaleEdgeStatementsByFilePath`, one statement per target
label, sequential, live proof `TestReducerRationaleEdgeRetractGraphTruth`).
Each remaining instance needs the same per-label + sequential rework with its
own live proof.

Further managed-transaction refinement (probed while fixing the
TAINT_FLOWS_TO retract): even a SINGLE `DELETE` statement dispatched through a
managed transaction (`ExecuteGroup`) can fail to apply on v1.1.11 — the same
statement auto-committed deletes the edge. Treat every retract `DELETE` as
auto-commit-only; grouped dispatch is safe only for MERGE-shaped writes.

Two orphan-cleanup shapes are also broken on v1.1.11 (probed while fixing the
shell-exec cleanup): a negated pattern predicate (`WHERE NOT (n)--()`) matches
nothing, so cleanups guarded by it silently keep every orphan; and an
`OPTIONAL MATCH (n)-[link]-() WITH n, link WHERE link IS NULL DELETE n`
pipeline returns the filtered row but does not apply the trailing `DELETE`
when the node previously had (since-deleted) relationships. A
`COUNT { (n)--() } = 0` predicate deletes the orphan correctly on both
v1.1.11 and the pinned Neo4j lane. The orphan-sweep subsystem still carries
the negated-pattern shape and is tracked separately.

Scope refinement (probed on v1.1.11 while fixing the rationale retract): the
zero-row behavior applies to a bare `MATCH` whose disjunction-labeled node is
filtered by a `WHERE` predicate, on either end of the pattern. A row-driven
`UNWIND $rows AS row MATCH (n:A|B|C {prop: row.value})` with an inline property
anchor DOES match and write correctly (the rationale EXPLAINS write template is
exactly this shape and creates every edge). Do not "fix" working UNWIND
inline-anchor writes; do fix every bare-MATCH disjunction retract or read.

Scope refinement (probed while backfilling the C-14 #4367 cloud-correlation
retracts): the managed-transaction-DELETE under-application is not limited to
multiple grouped statements. A **single** retract `DELETE` dispatched through
`ExecuteGroup` also under-applies on v1.1.11 — the same failure shape, just
with a group size of one. `KubernetesCorrelationEdgeWriter`,
`S3LogsToEdgeWriter`, `S3ExternalPrincipalGrantWriter`, and
`IAMInstanceProfileRoleEdgeWriter` each routed their single retract statement
through a shared `dispatch()` helper that used `ExecuteGroup` whenever the
executor implemented `GroupExecutor` — the shape `cmd/reducer` wires
unconditionally for every graph backend including NornicDB
(`reducerNeo4jExecutor.ExecuteGroup`). Fixed the same way as the per-label
retracts: each writer gained (or, for `SecurityGroupReachabilityWriter`,
reused) a `dispatchRetract` helper that always runs `Execute` sequentially,
never `ExecuteGroup`, live-proven in
`go/internal/storage/cypher/evidence-4367-cloud-edge-retract.md`. Do not
assume a single-statement group is safe from this class; the safe rule is
"retract DELETEs run through `Execute`, never `ExecuteGroup`," independent of
statement count.

That same backfill also resolved an open question about
`retractSecurityGroupSGRuleEdgesCypher`'s untyped relationship expansion
(`MATCH (sg:CloudResource)-[rel]->(rule:SecurityGroupRule) WHERE ... DELETE
rel`, anchoring on an unbound `[rel]` rather than a typed or disjunction
relationship pattern): probed directly over the HTTP `tx/commit` auto-commit
endpoint against a lean v1.1.11 container, seeding one
`CloudResource-[:ALLOWS_INGRESS]->SecurityGroupRule` edge and running the
exact retract statement as a single auto-commit statement deleted it (count 1
-> 0). The untyped-expansion shape itself is sound on this pinned version; it
was never the anchor pattern at fault, only the `ExecuteGroup` dispatch above.

### Validation

Run the static shape guard (no backend) and the backend-required retract proof
against the canonical v1.1.11 pin:

```bash
cd go
go test ./internal/storage/cypher -run TestCodeCallRetractStatementsUseSingleSourceLabel -count=1
ESHU_REPLAY_TIER_LIVE=1 bash ../scripts/verify-replay-tier.sh   # TestReducerCodeCallEdgeRetractGraphTruth, v1.1.11
```

No-Regression Evidence: the broken retract was a no-op (deleted nothing), so the
#5116 fix has no slower prior path to regress; the fix makes the intended scoped
retract work. `TestReducerCodeCallEdgeRetractGraphTruth` proves the in-scope
`CALLS`/`REFERENCES`/`INSTANTIATES` edges retract to zero while an out-of-scope
repo's edge and every endpoint node survive, on a real v1.1.11 NornicDB. The
per-label fan-out runs a bounded, fixed number of scoped deletes per retract.

No-Observability-Change: no runtime metric, span, log field, queue stage, worker
knob, or schema phase changes. The existing canonical retract spans and
graph-write failure/retry telemetry continue to expose retract behavior.

## When To Patch NornicDB

Patch NornicDB only when evidence supports one of these:

- a correctness fix for NornicDB itself
- a measured NornicDB performance win that generalizes beyond one Eshu symptom
- a measured Eshu runtime win proven by focused and corpus-level evidence

Before drafting a patch:

1. Write a failing test in `NornicDB-New`.
2. If the bug does not reproduce in NornicDB isolation, investigate Eshu first.
3. Build the patched binary into a unique image tag and pin that image only in
   the relevant test or Compose overlay.
4. Never overwrite a shared production image tag for a local experiment.
