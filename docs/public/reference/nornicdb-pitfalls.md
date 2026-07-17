# NornicDB Behavior and Pitfalls Reference

This page is the operational companion to
[NornicDB Tuning](nornicdb-tuning.md). It records NornicDB storage, schema,
constraint, and transaction behaviors that have affected Eshu integration and
proof work.

Cypher **query-shape** pitfalls — label disjunctions, empty-first-branch
unions, outer aggregation over `CALL {}`, and multi-clause reads — live in the
companion [NornicDB Query-Shape Pitfalls](nornicdb-query-pitfalls.md).

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
