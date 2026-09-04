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

## Which Build "The Pinned Build" Means Here

The same scoping note applies as on the companion page: entries below name
`nornicdb-cpu-bge:v1.1.11` (`sha256:51b6174a…`) or a `NornicDB-New` fork
checkout, measured when v1.1.11 was what `deploy/helm/eshu/values.yaml`
shipped. #6296 moved the chart to `v1.2.3@sha256:4dfa887d…`, a build that
self-reports version `1.2.2`, and these behaviors have not been re-measured on
it. Treat an entry as a reason to check the digest you actually run, not as a
statement about it.

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

On the pinned backend, a retry-safe `MERGE` containing `ON CREATE SET` can
instead surface the same commit failure under
`Neo.ClientError.Statement.SyntaxError`. NornicDB's retry-safety analyzer
currently counts `CREATE` inside that modifier as an independent `CREATE`
clause, so the server does not promote the error to its transaction code even
though the statement itself remains an idempotent `MERGE`.

That is normal concurrent `MERGE` behavior. Re-executing the same MERGE after
the winning commit should match the existing node.

### Eshu status

Eshu handles this in `go/internal/storage/cypher/retrying_executor.go`, with
the group replay-safety predicates in `writer.go` beside the `Statement` type
they reason about.
`RetryingExecutor.Execute` retries commit-time unique conflicts for a
MERGE-shaped statement, and `ExecuteGroup` does the same only when every
statement in the group converges on re-execution (`allStatementsAreReplaySafe`).
It also retries NornicDB's
`UNWIND MERGE chain relationship update failed: not found` snapshot conflict
when the typed error code is `Neo.ClientError.Statement.SyntaxError`.

Two statement shapes converge. A MERGE-shaped statement does by definition. So
does a predicate-scoped retract — `OperationCanonicalRetract` on Cypher that
opens with `MATCH`, whose only write clauses are `DELETE` or `REMOVE`, and
whose predicates are bounded by the bound parameters rather than by their
complement — because deleting whatever currently matches a key space the
parameters enumerate removes the same set on a second run. Anything else keeps
the group terminal: re-executing a `CREATE` duplicates, an accumulating `SET`
double-applies, and a row-driven `UNWIND ... MATCH ... DELETE` is the shape
that no-ops inside a managed transaction (see the retract pitfall above), so it
must never be replayed as though it had applied.

An open-ended predicate keeps the group terminal for a different reason. A
retract selecting the complement of a parameter — `n.generation_id <>
$generation_id`, `NOT (n.path IN $paths)` — or naming no parameter at all has
no fixed key space: rows a concurrent writer commits outside the parameter
values fall INTO range. Its match set therefore grows between a failed attempt
and its replay, and the replayed `DELETE` can remove rows the first attempt
never saw, which is exactly the "removes the same set" premise the retract
shape rests on. Those groups take dead-letter redrive instead. The check is
syntactic and fail-closed: it proves a predicate is bounded, it does not prove
an unbounded one is unsafe in a given deployment. A refused group loses a retry
it might not have needed and costs time; accepting a predicate whose match set
grows under concurrency costs graph truth.

This gate is repo-wide. `allStatementsAreReplaySafe` sits on the shared
`RetryingExecutor.ExecuteGroup` path, so it classifies every
`OperationCanonicalRetract` emitter, not only the semantic writer whose #6176
regrouping motivated it.

That retract shape was added for #6176. The semantic writer used to dispatch
its retract outside the group, so what reached the classifier was all-MERGE;
folding retract and upsert into one atomic transaction made the group mixed,
and a MERGE-only gate would have turned this very race into a dead-lettered
work item instead of a retried, converging write.

The retry classifier normally uses the typed Neo4j error code
`Neo.ClientError.Transaction.TransactionCommitFailed` or
`Neo.TransientError.Transaction.Outdated`, then validates the unique-conflict
body. For the pinned backend's compatibility shape, it accepts
`Neo.ClientError.Statement.SyntaxError` only when the message also contains
the observed `commit failed: constraint violation` prefix and the complete
UNIQUE-conflict body (`constraint violation`, `UNIQUE on`, and
`already exists`). The caller must still prove the statement is MERGE-shaped;
ordinary syntax errors and writes that do not converge on replay remain
terminal. Untyped or wrapped errors keep the historical fallback for
`failed to commit implicit transaction` and
`commit failed: constraint violation` shapes.

No-Regression Evidence: `go test ./internal/storage/cypher -run
'RelationshipSnapshot|PlatformCommitUniqueConflict|TestRetryingExecutor(ClassifiesTypedNornicDBTransactionCommitFailedByCode|RetriesNornicDBMergeUniqueConflict|RetriesNornicDBMergeUniqueConflictV1045Format|ExecuteGroupRetriesOnCommitTimeUniqueConflict|ExecuteGroupDoesNotRetryNonIdempotentStatements)|IdempotentRetract|NonIdempotentGroups|SemanticEntityWriterGroupedRetractConverges'
-count=1` proves typed error-code classification, historical substring
fallbacks, MERGE-only group retry, the #6176 idempotent-retract group retry,
and non-convergent-group non-retry behavior.
`scripts/verify_backend_conformance_live.sh` now runs
`TestLiveNornicDBRetryConflictClassificationContract` and
`TestLiveNornicDBRelationshipSnapshotConflictRetryContract` only in the
NornicDB live backend lane, where the pinned service must still surface each
retry-classifiable conflict and the relationship replay must converge to one
edge.

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
through the retrying executor. If the error reappears, verify
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

## Pitfall: Every Relationship-Existence Predicate Is Mis-Evaluated

### Observed shape

On both NornicDB backends used for the recorded proof (v1.1.11 and the former
PR #261 Compose image), every Cypher shape that asks "does this node have any
relationship" without binding a concrete relationship variable is wrong:

- `NOT (n)--()` (intended: "n has no relationship") always evaluates false --
  it matches nothing, ever, even for a node with zero relationships.
- `(n)--()` (intended: "n has a relationship") always evaluates true -- it
  matches every node, even one with zero relationships.
- `COUNT { (n)--() } = 0` (intended: "n has no relationship") always
  evaluates true -- the subquery's count is always reported as 0, so the
  predicate matches every node regardless of actual relationship state.

Eshu's orphan sweep (`go/internal/storage/cypher/orphan_sweep.go`, #5147) was
built on the first shape and was a silent no-op: the mark and sweep writes
never matched a true orphan, and the `eshu_dp_graph_orphan_nodes` gauge
reported a constant 0 regardless of how many disconnected nodes existed. The
same class of bug affected the `ShellCommand` orphan cleanup in
`edge_writer_shell_exec.go` (documented in
`go/internal/storage/cypher/evidence-4367-content-edge-retract-sequential.md`,
which originally and incorrectly claimed the `COUNT { (target)--() } = 0` form
"works" -- that claim proved only that the DELETE fired, never that it
preserved connected nodes and excluded true orphans; the same predicate class
now known to be a permanently-true tautology mis-classified it as a fix).

### Eshu implications

Do not write, review, or approve any Cypher shape in this repo that asks a
relationship-existence question without a concrete relationship variable. The
only proven-reliable primitive is a MATCH with a bound relationship variable
anchored on a specific node identity, for example:

```cypher
MATCH (n:Label {id: $id})-[r]-(m)
RETURN count(r) AS relationship_count
```

For a bounded batch of candidate nodes, anchor on their identity keys via
`UNWIND` rather than scanning the whole label:

```cypher
UNWIND $keys AS candidate_key
MATCH (n:Label {id: candidate_key})-[r]-(m)
RETURN DISTINCT n.id AS key
```

Then compute the anti-join (candidates minus connected) in application code,
not in Cypher. Eshu's orphan sweep now works this way; see
`go/internal/storage/cypher/README.md` ("OrphanSweepStore is the cleanup
seam...") and `evidence-5147-orphan-sweep-antijoin.md` for the full design and
live proof.

### Pitfall within the pitfall: UNWIND variable shadowing the RETURN alias

While proving the anti-join replacement, reusing the `UNWIND` binding
variable's name as the `RETURN ... AS` alias silently broke the query on both
proof backends:

```cypher
-- BROKEN: returns zero rows on both proof backends, no error
UNWIND $keys AS key
MATCH (n:Label {id: key})-[r]-(m)
RETURN DISTINCT n.id AS key
```

```cypher
-- CORRECT: distinct variable name for the UNWIND binding
UNWIND $keys AS candidate_key
MATCH (n:Label {id: candidate_key})-[r]-(m)
RETURN DISTINCT n.id AS key
```

Real Neo4j Cypher generally rejects redeclaring a bound variable name with a
compile error; NornicDB instead silently returns an empty result set. Always
give the `UNWIND` binding variable and any `RETURN ... AS` alias distinct
names, and do not trust an empty result from a NornicDB query as proof of "no
matching rows" without checking for this shadowing shape first.

### Validation

`go test ./internal/storage/cypher -run
'TestBuildConnectedKeysQueryUsesConcreteRelationshipVariable|TestLiveOrphanAntiJoinReplacesBrokenNotDashDashPredicate'
-count=1` (the second env-gated on `ESHU_CYPHER_BOLT_DSN`) proves the
concrete-relationship-variable form and the UNWIND/alias distinction hold, and
that the anti-join correctly detects a true orphan that the old `NOT
(n)--()` predicate silently ignored, on the pinned v1.1.11 and former PR #261
Compose images used for that proof.

## Pitfall: `RETURN DISTINCT` After A Trailing `OPTIONAL MATCH` Is Not Parsed

### Observed shape

Measured on the pinned v1.2.3 replay image
(`timothyswt/nornicdb-cpu-bge@sha256:4dfa887d990bf0b536693830830e34351c036716b0fe6dc957e1a3680e9f3c74`,
self-reporting 1.2.2) through `neo4j-go-driver/v5`, while proving the #5167
dead-code incoming-edge probe.

When a primary `MATCH` binds a relationship pattern and a trailing
`OPTIONAL MATCH` follows it, `RETURN DISTINCT` is not recognised as a modifier.
The keyword is absorbed into the first projection's source text, and no
deduplication happens at all:

```cypher
-- BROKEN: incoming_entity_id comes back as "DISTINCT coalesce(e.uid, e.id)"
MATCH (e:Function {uid: $id})<-[rel:CALLS]-(source)
OPTIONAL MATCH (source)<-[:CONTAINS]-(:File)<-[:REPO_CONTAINS]-(source_repo:Repository)
RETURN DISTINCT coalesce(e.uid, e.id) AS incoming_entity_id,
       rel.resolution_method AS resolution_method,
       (source_repo IS NOT NULL AND source_repo.id IN $allowed) AS in_grant

-- CORRECT: same statement with count(*) supplying the grouping
MATCH (e:Function {uid: $id})<-[rel:CALLS]-(source)
OPTIONAL MATCH (source)<-[:CONTAINS]-(:File)<-[:REPO_CONTAINS]-(source_repo:Repository)
RETURN coalesce(e.uid, e.id) AS incoming_entity_id,
       rel.resolution_method AS resolution_method,
       (source_repo IS NOT NULL AND source_repo.id IN $allowed) AS in_grant,
       count(*) AS edge_count
```

It is `DISTINCT` alone, not the surrounding shape. On the same statement and the
same seeded graph:

| Variant | Result |
| --- | --- |
| `RETURN DISTINCT coalesce(e.uid, e.id) ...` | first column is the literal `"DISTINCT coalesce(e.uid, e.id)"`, rows not deduplicated |
| `RETURN DISTINCT e.uid ...` (a plain property) | first column is the literal `"DISTINCT e.uid"` |
| `RETURN coalesce(e.uid, e.id) ...` (no `DISTINCT`) | every column evaluated, including the boolean and the `OPTIONAL MATCH`'s own `source_repo.id` |
| `RETURN ..., count(*)` (no `DISTINCT`) | every column evaluated, rows grouped by the non-aggregated projections |
| `OPTIONAL MATCH ... WITH ... RETURN DISTINCT ...` | worse: first column is the literal, every other column `null` |

A pattern comprehension used to avoid the `OPTIONAL MATCH` entirely
(`size([(source)<-[:CONTAINS]-(:File)<-[:REPO_CONTAINS]-(r:Repository) WHERE ... | 1]) > 0`)
is also not evaluated per row: it returned `true` for every source, including
ones with no repository path at all.

### Eshu implications

`buildDeadCodeScopedIncomingBatchProbeCypher`
(`go/internal/query/code_dead_code_candidate_entity.go`) is exactly this shape:
it expands a dead-code candidate's incoming edges, optionally matches the source
repository, and projects the caller's grant per row. It groups with `count(*)`
rather than `RETURN DISTINCT` for this reason, and the `count(*)` column is
never read -- it is the grouping vehicle. Do not "simplify" it back to
`DISTINCT`, and do not put a `WITH` between the `OPTIONAL MATCH` and the
`RETURN`.

The unrestricted probe beside it, `buildDeadCodeIncomingBatchProbeCypher`, does
keep `RETURN DISTINCT`, and correctly: nothing follows its anchoring `MATCH`, so
it is not the affected shape.

### Validation

`go test ./internal/query -tags live_nornicdb_dead_code_incoming -run
TestLiveNornicDBDeadCodeIncoming -count=1` against the pinned image covers every
row of the table above, plus the shape the probe ships: correctly grouped rows
from the `count(*)` form, the collapse in the pair of statements it replaced,
both `DISTINCT` corruptions, the `WITH` variant's nulled columns, and the
pattern comprehension's wrong answer.

Run it by hand. No CI job builds that tag, so nothing tells you on its own when
this backend behaviour changes — run it against the pin before changing a query
of this shape, and again after moving the pin. If
`TestLiveNornicDBDeadCodeIncomingRejectsReturnDistinct` fails, the executor
boundary has moved and the shapes above need re-measuring rather than a quick
edit. See `docs/internal/evidence/5167-code-family-batch-1.md` for the
before/after and the fan-in timing.

## Pitfall: `OPTIONAL MATCH` + Aggregate Collapses Every Zero-Match Group Into One Row

### Observed shape

Measured directly over the HTTP `tx/commit` endpoint and independently via
`neo4j-go-driver/v5` (both paths reproduce it) against the then-current
`eshu-nornicdb-pr261:149245885258` pin:

```cypher
CREATE (:Package {uid:"pkg:mini:1", ecosystem:"npm-mini", normalized_name:"a"});
CREATE (:Package {uid:"pkg:mini:2", ecosystem:"npm-mini", normalized_name:"b"});
MATCH (p:Package {ecosystem:"npm-mini"})
OPTIONAL MATCH (p)-[:HAS_VERSION]->(v:PackageVersion)
RETURN p.uid AS id, count(v) AS vc ORDER BY p.uid
-- expected 2 rows (both vc=0); ACTUAL 1 row
```

openCypher requires grouping by every non-aggregate `RETURN` key (here,
`p.uid`), so a correct implementation returns one row per matched `p`. On the
pinned NornicDB build the statement instead collapses to **at most one row
total** the instant any group's optional side is null — not just wrong counts,
a wrong row count. With mixed data (some packages with versions, some
without), only the row for the alphabetically-first `p.uid` survives, and it
can carry a count that belongs to a *different* package: seeding a third
package with 2 real `HAS_VERSION` edges produced a single output row
`{id: "no-versions-a", vc: 2}` — the id of the first zero-version package
paired with the version count that actually belongs to the third package.

### Eshu implications

Any handler composing `OPTIONAL MATCH` with an aggregate (`count()`, `sum()`,
`collect()`, etc.) over the anchor's non-aggregate columns silently drops
every zero-match row instead of returning it with a zero/empty aggregate.
`packageRegistryPackagesCypher` (`go/internal/query/package_registry_cypher.go`,
issue #5167) served this exact shape for
`GET /api/v0/package-registry/packages`: a zero-version `Package` vanished
from every ecosystem-scoped list read, and an exact `package_id` lookup for a
zero-version package returned an empty page — indistinguishable from "package
does not exist."

The F-6/W5b tenant-scoped ecosystem-browse variant,
`packageRegistryPackagesScopedEcosystemCypher`, reused the identical
`OPTIONAL MATCH (p)-[:HAS_VERSION]->(v) ... count(v)` composition (with the
combined `WHERE p.ecosystem = $ecosystem AND p.visibility = 'public'`
predicate from the pitfall below) and was exposed to the same row-collapse:
a public, zero-version package would silently vanish from a scoped caller's
ecosystem browse. It was rewritten to the same anchor-only + `UNWIND`
version-count split before it shipped, confirmed live: the pre-fix scoped
shape collapsed a 2-public-package fixture (one zero-version, one
two-version) to a single row, with the two-version package's count leaking
onto the zero-version package's id — the same wrong-id/count pairing as the
unscoped shape's evidence above.

Two candidate single-statement replacements were tried and also rejected —
each looked correct in isolation but broke as soon as a non-zero-count row was
added to the fixture:

- `size([(p)-[:HAS_VERSION]->() | 1]) AS version_count` (pattern
  comprehension): correctly returned all packages with `vc=0` for the
  zero-version case, but always evaluates to `0` even for a package with real
  edges — a second, independent NornicDB defect (confirmed live: the
  comprehension undercounts even though a plain `MATCH` on the same node
  correctly returns both edges).
- `OPTIONAL MATCH (p)-[:HAS_VERSION]->(v) WITH p, collect(v) AS versions
  RETURN size(versions)`: the same "extra clause between the anchoring
  `MATCH`/`OPTIONAL MATCH` and the final `RETURN`" shape covered by the
  "Multi-Clause Read Queries Silently Corrupt The Projection" pitfall in
  [NornicDB Query-Shape Pitfalls](nornicdb-query-pitfalls.md); also always
  returns `0`.

The fix that measured correctly in every case (0-version, mixed 3-package,
and a 200-package/100-with-version corpus) is a separate, single-clause,
inner-join `MATCH` scoped to the already-resolved page via `UNWIND`, merged in
Go with the anchor read (the established "run as a SEPARATE single-clause
query merged in Go" pattern from the relationship-existence pitfall above):

```cypher
UNWIND $package_ids AS candidate_package_id
MATCH (p:Package {uid: candidate_package_id})-[r:HAS_VERSION]->(v:PackageVersion)
RETURN p.uid AS package_id, count(r) AS version_count
```

Any package uid absent from this query's result has zero matches; the caller
zero-fills it (`packageRegistryVersionCountsCypher` +
`PackageRegistryHandler.attachPackageVersionCounts` in
`go/internal/query/package_registry.go`). Do not reintroduce
`OPTIONAL MATCH` + aggregate over an anchor's own projected columns on this
backend; do not "fix" it with a pattern comprehension or a `WITH`+`collect`
without proving it live first, both silently under-count in a way that looks
correct on a same-cardinality-only fixture.

### Validation

`go test ./internal/query -run
'TestLivePackageRegistry(ListPackagesReturnsZeroVersionPackages|ScopedEcosystemBrowseReturnsZeroVersionPackages)'
-count=1 -v` (env-gated on `ESHU_PKG_REGISTRY_PROVE_LIVE=1` and
`ESHU_NEO4J_URI`) is the backend-required live proof for both the unscoped
and scoped-ecosystem branches: each captures its OLD `OPTIONAL MATCH`+
`count(v)` shape's output for evidence, then asserts the shipped handler
returns every package (including zero-version ones with `version_count: 0`,
and, for the scoped variant, excluding the private package) and, for the
unscoped branch, resolves a zero-version package by exact `package_id`. See
`docs/internal/evidence/5167-package-registry-version-count-nornicdb.md` for
full before/after tables including the rejected candidates, the
200-package corpus timing, and the scoped-ecosystem before/after.

## Pitfall: Inline `MATCH` Property Pattern Silently Dropped By A Trailing `WHERE`

### Observed shape

On the former PR #261 Compose image, combining an inline
property pattern on a `MATCH` with a `WHERE` clause that filters a DIFFERENT
property silently drops the inline pattern's filter -- the query falls back to
an unfiltered label scan instead of erroring or returning an unfiltered-but-
still-labelled result:

```cypher
-- BROKEN: $ecosystem is silently ignored. Returns the SAME total for every
-- $ecosystem value (verified: a 120k-node "npm-shimb" partition and a
-- disjoint "npm-shima" partition both returned the count of ALL
-- visibility='public' Package nodes across BOTH partitions, not just the
-- $ecosystem-matching partition).
MATCH (p:Package {ecosystem: $ecosystem})
WHERE p.visibility = 'public'
RETURN count(p) AS c
```

```cypher
-- CORRECT: combine both predicates in one WHERE clause; do not mix an inline
-- MATCH property with a trailing WHERE on an unrelated property.
MATCH (p:Package)
WHERE p.ecosystem = $ecosystem AND p.visibility = 'public'
RETURN count(p) AS c
```

Reproduced identically via the HTTP tx/commit endpoint (`/db/nornic/tx/commit`)
AND the real Bolt protocol via `github.com/neo4j/neo4j-go-driver/v5` (the same
driver `go/internal/query/neo4j.go`'s `Neo4jReader.Run` uses in production) --
this is not an HTTP-transport artifact.

### Eshu implications

This is both a correctness bug (a cross-partition/cross-tenant leak: a query
meant to be anchored on one label-property value instead scans and returns
matches from every value of that property) and a latent performance
regression (the intended selective anchor is defeated, forcing a full label
scan). Found while proving the F-6/W5b (#5167) tenant-scoping theory for
`packageRegistryPackagesCypher`'s ecosystem-browse branch
(`go/internal/query/package_registry_cypher.go`,
`packageRegistryPackagesScopedEcosystemCypher`), which was designed against
this exact composition and had to be rewritten to the WHERE-only combined form
before it could ship. Never append a `WHERE` clause referencing a different
property onto a `MATCH` that carries an inline pattern property; move ALL
selectivity predicates into one `WHERE` clause instead.

### Validation

`go test ./internal/query -run
'TestPackageRegistryPackagesScopedEcosystemBrowseUsesVisibilityFilteredCypher'
-count=1` asserts the shipped scoped-ecosystem-browse Cypher text uses the
combined-`WHERE` form and explicitly rejects the inline-pattern-plus-trailing-
`WHERE` shape.

## Pitfall: `EXISTS {}` Subquery Correctness Depends On Anchor Direction And Hop Count

### Observed shape

On the former PR #261 Compose image, `EXISTS { MATCH (pattern)
WHERE (filter on the far variable) }` evaluates correctly only for one specific
shape. Measured against representative and worst-case fixture data (500-1000
row fan-out) while redesigning the infra scoped-token authorization predicate
(#5384):

- **Correct:** a forward, single-hop `EXISTS` anchored on the bound node `n`,
  filtering the far variable with an `IN $array` comparison:
  `EXISTS { MATCH (n)-[:DEPLOYMENT_SOURCE]->(r:Repository) WHERE r.id IN $g }`.
  TRUE case matches, FALSE case does not -- this is the only `EXISTS` shape
  proven reliable on this backend.
- **Broken -- always TRUE (whole-graph leak):** an `n`-first `EXISTS` with the
  arrow pointing backward into `n`, filtering the far variable:
  `EXISTS { MATCH (n)<-[:USES]-(i:WorkloadInstance) WHERE i.repo_id IN $g }`.
  This matches every node regardless of whether the filter is satisfied,
  silently authorizing every row it is meant to gate.
- **Broken -- always FALSE (dead code / under-authorization):** an `n`-last
  multi-hop `EXISTS` bridge, filtering the near variable instead of `n`:
  `EXISTS { MATCH (r:Repository)-[:DEFINES]->(:Workload)<-[:INSTANCE_OF]-(:WorkloadInstance)-[:USES]->(n) WHERE r.id IN $g }`.
  This matches nothing, ever, even for genuinely in-grant nodes -- the
  predicate silently drops every row it should admit.

Both broken shapes were reproduced identically via the HTTP `tx/commit`
endpoint and the real Bolt protocol via `github.com/neo4j/neo4j-go-driver/v5`
(the production driver), so this is not an HTTP-transport artifact.

### Eshu implications

Do not use an `EXISTS {}` subquery to express "is `n` reachable from a granted
node" unless the shape is forward-anchored, single-hop, with the `IN $array`
filter on the far variable (the one correct shape above). For every other
reachability direction or hop count, use a pattern-predicate evaluated
directly as a boolean -- not wrapped in `EXISTS {}` -- with an inline-map
property term per candidate value, for example
`(n)<-[:USES]-(:WorkloadInstance {repo_id:$g})`. This form is correct on both
NornicDB and Neo4j and trades reliability for O(grant) fan-out (one term per
candidate value) instead of O(1).

The infra scoped-token authorization predicate
(`go/internal/query/infra_scope_grant.go`, `infraResourceScopePredicate`) is
built this way: `scopeGrantInlineMapDisjunction` renders the inline-map
OR-chain for the CloudResource-via-USES and Workload-via-DEFINES admission
paths (both previously shipped as the always-FALSE `n`-last bridge shape
above, which silently under-authorized every scoped CloudResource and
name-collision Workload), while the WorkloadInstance-via-DEPLOYMENT_SOURCE
admission path keeps the one correct forward-anchored `EXISTS` shape.
`maxScopeGrantInlineTerms` caps the inline-map fan-out with fail-closed
degradation: past the cap, a token still sees every resource it directly owns
(O(1) flat `repo_id` / `id` disjuncts), and only loses collision/bridge
admission for grants beyond the cap -- an under-authorization, never a leak.

### Validation

`go test ./internal/query -run
'TestInfraResourceScopePredicateRendersOnlyWhenScoped' -count=1` asserts the
shipped predicate text contains the inline-map disjuncts and the one correct
forward `EXISTS` disjunct, and explicitly rejects both broken `EXISTS` shapes
(the `n`-last `DEFINES`/`INSTANCE_OF` bridge and the `n`-first backward `USES`
form) from ever reappearing in the rendered Cypher.

## Pitfall: `UNWIND`-Batched Bare-`MATCH` `SET` Silently Drops Its Write

### Observed shape

On `nornicdb-cpu-bge:v1.1.11` (the chart's pin when this was measured), an
`UNWIND`-batched statement whose anchor clause is a bare, property-keyed
`MATCH` — with no `MERGE` anywhere in the statement — silently drops its
`SET`:

```cypher
-- BROKEN: reports success, the property is never persisted.
UNWIND $rows AS row
MATCH (resource:CloudResource {uid: row.uid})
SET resource.some_property = row.value
```

The node matches (a separate read confirms it). The statement completes with
no error. Batched-write counters are not a reliable signal either way:
`PropertiesSet` and `ContainsUpdates` both report the "nothing happened"
shape (`0`/`false`) on this no-op, but they are equally unreliable in
general on this backend — do not trust them as proof of success OR failure.
The only reliable proof is a read-back of the property in a separate
transaction. The identical statement anchored with `MERGE` instead of `MATCH`
persists correctly:

```cypher
-- CORRECT: persists.
UNWIND $rows AS row
MERGE (resource:CloudResource {uid: row.uid})
SET resource.some_property = row.value
```

A single-property `UNIQUE` constraint on the anchored property does **not**
fix the no-op — this was tried and measured broken before the `MERGE` fix
was found.

### Eshu implications

`MATCH` is not a safe substitute for `MERGE` purely on correctness grounds
here, but `MERGE` is also not a safe *blind* substitute for a writer with a
never-create contract: `MERGE` unconditionally creates on a miss. Issue
#5652 found this broke all four AWS posture node writers
(`go/internal/storage/cypher/{ec2_internet_exposure,ec2_block_device_kms_posture,rds_posture,s3_internet_exposure}_node_writer.go`),
each of which must only update an already-materialized `CloudResource` node
and must never fabricate one for a uid that was never admitted.

The fix is a two-phase write: read which candidate identities already exist
via a separate query first (reads are not subject to this no-op), drop rows
whose identity is not confirmed, and only then run the `MERGE`-anchored
write against the confirmed subset — so `MERGE` always matches and never
creates. See `go/internal/storage/cypher/posture_node_existence.go`
(`PostureExistenceReader`, `filterRowsToExistingCloudResourceUIDs`) for the
shared implementation and
`go/internal/storage/cypher/unwind_bare_match_set_gate_test.go` for the
static class-gate that fails any future `UNWIND`-batched bare-`MATCH`-then-
`SET` statement with no `MERGE` safety net anywhere in it.

Two other `UNWIND` shapes in the canonical File/Directory writer were flagged
as suspect during the #5652 investigation but re-verified separately on a
fresh, uncontaminated container and found NOT to be production bugs (details in
`docs/internal/evidence/5652-followup-file-directory-edge-writeloss-investigation.md`):

- A `WITH`-chained multi-clause File update (`UNWIND ... MATCH ... SET ... WITH
  ... MATCH ... MERGE`) appeared to drop its post-`WITH` edge MERGEs, but this
  did not reproduce in any production dispatch mode. **Root-caused in #5671: the
  variable is the transport, not the state of the stack.** Over the HTTP query
  endpoint `POST /db/<db>/tx/commit` the second post-`WITH` `MERGE` is dropped
  silently — `errors: []`, no edge — while the Bolt driver commits it, both
  measured on the same running v1.1.11 container. Splitting the same work into
  two single-`WITH` statements over that endpoint writes both edges, so the
  chained second `WITH` is the trigger.

  The earlier stack-contamination guess is **disproven**: the zero-edge result
  reproduces deterministically on a container started fresh with nothing else
  ever run on it. It was also a poor fit on symptoms — the constraint
  drop/recreate pitfall above fails **loudly** with a false `UNIQUE` violation,
  whereas this is a **silent** zero-row write.

  Practical rule: do not write a live graph probe against the HTTP query
  endpoint and read the result as production truth; it can manufacture a
  write-loss no production path exhibits. Use the Bolt driver, as the repo's
  live tests do. No fix ships and no rewrite is needed — production is Bolt.
  Evidence:
  `docs/internal/evidence/5652-followup-file-directory-edge-writeloss-investigation.md`.
- `UNWIND`-batched `MATCH ... DELETE` (the retract/refresh edge-cleanup
  statements) no-ops only under the atomic `ExecuteGroup` managed-transaction
  path. Production routes these with `OperationCanonicalRetract` through
  `PhaseGroupExecutor.executeSequentialRetractPhase`, which always uses
  sequential auto-commit `Execute`, never `ExecuteGroup` — so the shape is
  production-unreachable there. If you ever route a retract through
  `ExecuteGroup`, rewrite the batch shape from `UNWIND` to `WHERE ... IN`
  (`MATCH (f:File) WHERE f.path IN $file_paths MATCH (f)-[r:IMPORTS]->(:Module)
  DELETE r`), which was proven to delete correctly. The underlying backend
  shapes are tracked upstream as #4902 and #5323. One retract does route
  through `ExecuteGroup`: the semantic entity retract, since #6176 removed the
  sequential split it carried while v1.1.11 was supported. That is safe for two
  independent reasons — its Cypher is already the `WHERE ... IN` shape rather
  than the `UNWIND`-batched one, and the grouped `DETACH DELETE` under-apply it
  used to work around is a v1.1.11 defect that does not reproduce on 1.2.1 or
  1.2.2 (measured 20/20 in
  `go/internal/storage/cypher/evidence-6176-semantic-retract-regrouped.md`).

### Validation

`go test ./internal/storage/cypher -run
'TestLivePostureNodeWritersPersistAndNeverCreate'
-count=1` (env-gated on `ESHU_CYPHER_BOLT_DSN`) drives all four posture node
writers' real production Cypher against a live NornicDB and proves, by
read-back in a separate transaction, that the write persists for a
confirmed-existing uid and that a never-confirmed uid creates no phantom
node. `go test ./internal/storage/cypher -run
'TestNoUnwindBareMatchThenSetCyphersInPackage' -count=1` is the static
class-gate.

## Not A Pitfall: Bounded `ORDER BY ... LIMIT` Selects A Deterministic Top-N

Eshu's bounded reads pair a total-order `ORDER BY` with a sentinel `LIMIT`
(`ORDER BY <total-order-tuple> LIMIT $sentinel`) and then re-sort and truncate
the returned rows in Go. That in-memory truncation is only sound if the backend
`LIMIT` **selects** a deterministic candidate subset containing the true
lexicographic top-N. An `ORDER BY` that merely ordered *delivery* of an
arbitrary subset would let the survivor set vary between identical calls once
the distinct candidate count exceeded the sentinel — a silent accuracy bug that
in-memory tests cannot catch, because they shuffle a fixed, already-selected row
set.

### Validation

Verified on NornicDB v1.1.11 for both plan shapes Eshu relies on, each driven
through its real production function at its real sentinel:

- Plain `MATCH ... ORDER BY ... LIMIT`: 120 distinct rows against a 51-row
  sentinel, 25 repeated calls.
- Aggregating `WITH ... collect() ... ORDER BY ... LIMIT`: 2,550 distinct rows
  against a 2,501-row sentinel, 5 repeated calls.

Rows are inserted in **reverse** lexicographic order, so a backend returning
scan order would produce a demonstrably wrong survivor set rather than an
accidentally-passing one. Each run asserts the survivors are both stable across
calls and exactly equal to a lexicographic top-N computed independently from
every seeded row — not from the subset the backend returned.

```bash
ESHU_SERVICE_STORY_DETERMINISM_NORNICDB_LIVE=1 \
ESHU_NEO4J_URI=bolt://localhost:37687 \
go test ./internal/query -run TestServiceStoryTruncationSelectionIsDeterministicLiveNornicDB -count=1 -v
```

Keep the Go-level sort regardless: it is defense in depth against delivery-order
variance *within* the returned subset (the #5644 symptom), and it is what makes
the truncation independent of any future backend planner change. Re-run the
proof above when the pinned NornicDB version changes.

## Historical Pitfall: Trailing `OPTIONAL MATCH` Corrupted Function-Call Projections

### Observed shape

On both older NornicDB backends used for the recorded proof (v1.1.11 and the former
PR #261 Compose image), a read query whose primary `MATCH` binds a relationship
pattern, followed by one or more trailing `OPTIONAL MATCH` clauses with no
`WITH` in between, returns
every function-call expression in the `RETURN` as its **literal source text**
instead of the evaluated value. Reproduced identically via the HTTP
`/db/nornic/tx/commit` endpoint and the real Bolt driver
(`github.com/neo4j/neo4j-go-driver/v5`, the production driver):

```cypher
-- CORRECT (no OPTIONAL MATCH): type="INHERITS", source_id="cls:ServiceDog"
MATCH (e:Class {uid:$id})-[rel:INHERITS]->(target)
RETURN type(rel) AS type, coalesce(e.id, e.uid) AS source_id, target.name AS target_name

-- BROKEN (trailing OPTIONAL MATCH): type="type(rel)", source_id="coalesce(e.id, e.uid)"
MATCH (e:Class {uid:$id})-[rel:INHERITS]->(target)
OPTIONAL MATCH (e)<-[:CONTAINS]-(f:File)
RETURN type(rel) AS type, coalesce(e.id, e.uid) AS source_id, target.name AS target_name
```

Precise corruption boundary, measured on the affected older images:

- **Corrupt (returns the expression's literal text):** every function call —
  `type(rel)`, `coalesce(...)`, `head(labels(...))`, `labels(...)`, and
  aggregates like `count(...)`; the relationship variable itself (bare `rel`
  and `rel.prop` both return `"rel"` / `"rel.prop"`); and any property on a
  chained second-level `OPTIONAL MATCH` binding (`targetRepo.id` →
  `"targetRepo.id"`).
- **Survives (correct value):** plain properties on the primary `MATCH` node
  (`target.name`) and on a first-level `OPTIONAL MATCH` binding
  (`targetFile.relative_path`).

Chaining is not required — a single trailing `OPTIONAL MATCH` triggers it.
Putting a `WITH e, rel, target` between the `MATCH` and the `OPTIONAL MATCH`
does not rescue it; it swaps in a different silent corruption (the function
columns come back `nil` and rows duplicate). A node-only primary `MATCH` (no
relationship pattern) + `OPTIONAL MATCH` evaluates functions correctly, because
that shape routes to a different executor branch.

### Root cause

In the affected NornicDB source, a `MATCH ... OPTIONAL MATCH` with no leading
`WITH` routes to `executeCompoundMatchOptionalMatch`
(`pkg/cypher/clauses.go`). When the primary `MATCH` contains a relationship
pattern it takes the traversal branch, which resolves `RETURN` items with
`resolveReturnExprFromVarMap` — a resolver that understands only `var.prop` and
bare variables and falls back to returning the raw expression source text for
everything else — instead of the real evaluator `evaluateExpressionWithContext`
that the non-traversal branch (`buildJoinedResult`) uses. The same branch also
never binds the relationship variable, never routes aggregates to aggregation,
and only parses the first `OPTIONAL MATCH` clause. The `fail-loud` multi-clause
guard did not fire for this shape, so it corrupted silently.

### Fixed status

NornicDB PR #265 fixed the traversal/relationship-seeded function-projection
case and the relationship-seeded chained second-hop property case. The
implementation began at upstream commit
`883065cd744b835237f0a26bce0fd41883cd2b64` and was completed by
`e4b84afef25282ee8747c66c8fddb8fdff836d28`; both are ancestors of NornicDB
v1.2.3 commit `d9b76ae82334e6b23b847156eb81931781546b85`. Eshu's replay tier pins the
published v1.2.3 multi-architecture image by digest and requires evaluated
`type(rel)`, `coalesce(...)`, and relationship-seeded chained second-hop
property results.

The node-only compound path is not fixed in v1.2.3: when a primary node `MATCH`
is followed by two chained `OPTIONAL MATCH` clauses, the second-hop property
still returns its literal expression (`sourceRepo.id` → `"sourceRepo.id"`).
The replay tier retains this as a negative control, including an explicit null
guard. No production Eshu query uses that node-only probe.

### Eshu implications

On the affected older images, a handler that bound a relationship variable and
then added an `OPTIONAL MATCH` for enrichment silently lost `type(rel)` and
every `coalesce()`/`head()` identity column.
`nornicDBOneHopRelationshipsCypher`
(`go/internal/query/code_relationships_nornicdb.go`, issue #5681) formerly
served exactly this shape for `POST /api/v0/code/relationships` name/entity
lookups (IMPORTS, INHERITS, OVERRIDES, and CALLS direct callers/callees). The
corrupt `type` column never equalled the requested relationship type, so
`filterRelationships` dropped every edge and the route returned empty
`outgoing`/`incoming` even when the graph held correct edges — a silent false
negative, the worst failure class for this repo.

Eshu keeps the established split-and-merge pattern: the relationship core read
carries **no** `OPTIONAL MATCH`, so `type(rel)`, `coalesce(...)`, and
`head(labels(...))` all evaluate; file/repository/language enrichment is fetched
by two separate, `OPTIONAL-MATCH`-free, index-anchored single-path reads
(`code_relationships_nornicdb_enrich.go`) whose plain-property results are
joined to the core rows by endpoint uid in Go. Endpoints with no `File` simply
do not appear in the enrichment read (a left-join in Go), replacing the OPTIONAL
semantics without the OPTIONAL clause. The split also preserves bounded reads
and File metadata when a Repository edge is absent. Consolidating it is a
separate hot-query change that needs behavior and performance proof.

### Validation

`go test ./internal/query -run
'TestNornicDBIncomingOneHopCypherSeedsExactTarget|TestNornicDBOneHopRelationshipsCypherProjectsRelatedSymbolSourceMetadata'
-count=1` asserts the shipped core read contains no `OPTIONAL MATCH` and that the
enrichment reads carry the file/repo projection. `go test ./internal/query
-tags live_nornicdb_relationships_proof -run
'TestLiveNornicDBRelationshipsSurviveOptionalMatchProjection' -count=1` (against
the pinned image) is the backend-required live proof: it seeds a Class
inheritance graph and asserts the route returns three `INHERITS` edges with an
evaluated `type` and enriched file/repo metadata, where the pre-split shape
returned zero. See
`docs/internal/evidence/5681-nornicdb-optional-match-relationship-projection.md`
for the historical before/after and the isolated executor characterization.

The replay-tier tests
`TestNornicDBFunctionProjectionEvaluatesAfterOptionalMatch` and
`TestNornicDBChainedOptionalMatchPreservesExecutorBoundary` exercise the
measured boundary directly against v1.2.3. They require evaluated values for
the relationship-seeded shapes and require the exact literal-placeholder
negative control, never a missing or null column, for the node-only compound
path.

### The boundary is wider than "function-call projection"

Measured against the former PR #261 build while proving issue #5694:

| Historical shape | Column read | Old result |
| --- | --- | --- |
| no `OPTIONAL MATCH` | `type(rel)`, `coalesce(...)` | correct |
| one `OPTIONAL MATCH`, read its own variable | `f.relative_path` | correct |
| two chained, read the FIRST one's variable | `f.relative_path` | correct |
| two chained, read the SECOND one's variable | `r.id` — a plain property read | **`"r.id"`** |
| two chained, no relationship bound anywhere | `r.id` | **`"r.id"`** |

The v1.2.3 boundary separates the executor paths: relationship-seeded traversal
now evaluates both the function projections and the second chained property,
while the node-only compound path still corrupts the second chained property.
The function-call symptom above was therefore the narrower historical case;
plain property reads remain affected only on the measured node-only path.

`go/internal/query/code_relationship_story_nornicdb.go` still pairs every
second-hop column with its historical literal placeholder through
`nornicDBStoryProjection`. Its production relationship-seeded query sees
evaluated values on v1.2.3, so the guard is a no-op there; older or custom
backends still fail closed instead of serving expression text. Removing that
compatibility guard belongs with any measured query-shape consolidation, not
with the backend-proof update.

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

### How to patch: support the shape, mirror Neo4j, never fail loud

NornicDB's goal is drop-in Neo4j compatibility, and the maintainer has rejected
fail-loud patches more than once. When the fix is a Cypher executor bug:

- **Support every valid shape by mirroring Neo4j's semantics.** Do not fail
  loud, reject, or error on a shape Neo4j executes. Rejecting a valid query is
  not better than corrupting it. Teach the executor the shape; do not gate it
  with a guard that fires before the pipeline handoff and freezes valid queries
  out of the good executor.
- **Reference the Neo4j source.** The Cypher runtime lives under
  `community/cypher/` in the Neo4j monorepo. Use the checkout at
  `~/os-repos/neo4j`; clone it if absent (`git clone --filter=blob:none --sparse
  https://github.com/neo4j/neo4j ~/os-repos/neo4j`, then `git sparse-checkout set
  community/cypher`). Map the operator that governs the shape and cite it: for
  OPTIONAL MATCH that is `OptionalPipe` / `ApplyPipe` / `OptionalExpandAllPipe`
  (per left row, run the inner pattern; no match keeps the row and nulls the new
  variables); for aggregation it is `EagerAggregationPipe` and the front-end
  `isolateAggregation` rewrite.
- **Keep only the parse errors Neo4j itself raises** (for example `count()` with
  no argument). If unsure whether Neo4j accepts a shape, assume it does.

This is the standing contract; `.agents/skills/cypher-query-rigor` carries the
same rule for query authoring.

## Pitfall: Older Builds Ignore Relationship Properties In `MERGE` Identity

### Observed shape

Cypher says `MERGE (a)-[r:REL {k: 'one'}]->(b)` matches only a `REL`
relationship whose `k` is `'one'`, and creates one otherwise. NornicDB builds
before orneryd/NornicDB#290 ignored that property map while matching: the second
`MERGE` bound the relationship the first one created, so one edge existed where
the specification calls for two.

This was measured against the former `eshu-nornicdb-pr261:149245885258`
default while implementing issue #5691, through Eshu's own Bolt driver:

| Statement shape | Rows written | Edges after | `r.k` |
| --- | --- | --- | --- |
| two separate single `MERGE`s | 2 | **1** | `one` |
| one `UNWIND` batch, two rows | 2 | **1** | `one` |
| `UNWIND` batch, `k` also in the `SET` body | 2 | **1** | `two` |
| `UNWIND`, one row per statement | 2 | **1** | `one` |

Every shape collapses. This is not the `executeUnwindMergeChainBatch` fast
path — the single-statement case behaves identically — so splitting the batch,
shrinking `batchSize`, or re-running the statement does not recover the lost
edge. (Serializing the write would not fix it either; the second write matches
the first edge no matter how far apart the two run.)

### Why it matters

A writer that keys parallel relationships between the same two nodes on a
property does not get parallel relationships. It gets one edge whose properties
are whichever row the backend wrote last, with no error and no dead letter —
the same silent-empty failure class as the other pitfalls on this page, except
it under-reports rather than empties.

### Fixed default and deployment order

The default `docker-compose.yaml` source pin is now
`eshu-nornicdb-pr290:3722b483c02c` at merged full revision
`3722b483c02c38a8e046d198f8768f200f31023c`. The corrected backend includes
pattern properties in relationship `MERGE` identity for plain, batched, and
explicit-transaction paths. The #5827 live proof starts with one legacy
endpoint-only relationship and replays two same-pair assertions in both orders;
all four Eshu provenance shapes finish with exactly two relationships. Duplicate
and eight-writer concurrent replays converge, and retracting one assertion
leaves the other intact.

Roll out the corrected NornicDB build before an Eshu build that makes a
relationship property part of writer identity. Eshu migration 096 reopens the
current affected reducer work so normal retract-before-write processing repairs
legacy collapsed rows; deploying the writer first against an older backend
would preserve the silent collapse.

Endpoint modeling remains useful when the distinguishing value is a domain
entity in its own right. It is no longer required as a backend workaround on
the corrected PR #290 build.

### Historical impact on two shipped writers

`batchCanonicalCodeownersOwnershipEdgeCypher` (`DECLARES_CODEOWNER`, keyed on
`pattern`/`source_path`) and `batchCanonicalSubmodulePinEdgeCypher`
(`PINS_SUBMODULE`, keyed on `path`) were both authored against the Cypher
semantics, and each carries a doc comment asserting the property key is what
keeps their edges parallel. On Neo4j it does. Running their EXACT shipped
statements against the former PR #261 build:

| Writer | Rows written | Edges after | Surviving edge |
| --- | --- | --- | --- |
| `DECLARES_CODEOWNER`, patterns `/src/*` then `/docs/*` | 2 | **1** | `pattern=/src/*`, `order_index=2` |
| `PINS_SUBMODULE`, paths `vendor/a` then `vendor/b` | 2 | **1** | `path=vendor/a`, `pinned_sha=<vendor/b's sha>` |

The result is worse than a lost row. The surviving edge keeps the FIRST row's
merge-key properties and takes the SECOND row's `SET` properties, so it is a
blend of two different declarations: a `PINS_SUBMODULE` edge that says
`vendor/a` is pinned at `vendor/b`'s commit. Neither writer has a
backend-required test, so nothing catches it. A repository with two CODEOWNERS
patterns owned by one team, or two submodules pointing at one target
repository, is the ordinary case rather than a corner case.

On the corrected pin these statements retain both relationships and their
matching properties. Deployments still running an older NornicDB build must not
trust those edge counts or per-edge properties.

### Reproducing

Point the offline replay tier at a NornicDB and run the committed IMPORTS proof,
which fails on any regression of the fold:

```bash
ESHU_REPLAY_TIER_LIVE=1 NEO4J_URI=bolt://localhost:7687 \
NEO4J_USERNAME=neo4j NEO4J_PASSWORD=change-me NEO4J_DATABASE=nornic \
go test ./internal/replay/offlinetier -run TestCanonicalImportEdgesGraphTruth -count=1 -v
```

Re-run it when the pinned NornicDB version changes. The IMPORTS fold remains a
valid narrowing even though the backend now supports parallel property-keyed
relationships.

## Pitfall: A Zero-Row Relationship `DELETE` Still Costs Proportional To Store Size

A `DELETE rel` whose `MATCH` selects nothing is not free. Its cost tracks the
size of the store, not the number of rows it removes, so a no-op retract that is
instant in a test fixture becomes seconds on a production-sized graph.

This is a backend defect, tracked upstream as
[orneryd/NornicDB#296](https://github.com/orneryd/NornicDB/issues/296). What
follows is how to work around it until that lands, not a shape to design
toward.

Measured on the pinned build, two stores, same image, same statement, same
parameters, all deleting zero rows. Trials interleaved between the stores so
cache warmth could not land entirely on one:

| statement (all match zero rows) | 1,675,949 relationships | empty store | ledger |
| --- | --- | --- | --- |
| `MATCH (r:Rationale)-[rel:EXPLAINS]->() WHERE r.repo_id IN $ids AND rel.evidence_source IN $srcs DELETE rel` | 18.603s / 17.653s / 18.071s | 0.021s / 0.022s / 0.023s | `ledger:5998-zero-row-explains-delete-large-store`, `ledger:5998-zero-row-explains-delete-empty-store` |
| `MATCH (r:Rationale)-[rel:EXPLAINS]->() DELETE rel` | 17.666s / 16.944s / 17.540s | 0.021s / 0.021s / 0.025s | `ledger:5998-zero-row-explains-delete-source-anchored` |
| `MATCH ()-[rel:EXPLAINS]->() DELETE rel` | 173.185s / 177.124s / 182.249s | not recorded | `ledger:5998-zero-row-explains-delete-untyped-both-ends` |
| `MATCH (r:Rationale)-[rel:EXPLAINS]->() WHERE ... RETURN rel LIMIT 1` (pre-change shape) | 0.021s (3 trials) | 0.021s (3 trials) | `ledger:5998-explains-existence-probe-read` |

The per-label DELTA retract carries the same store-size term. Its seven
statements, one per target label, cost 12.589s together on a
190,000-relationship store while deleting zero rows, against 0.291s on an empty
one (`ledger:5998-delta-per-label-retract-seeded-rerun`,
`ledger:5998-delta-per-label-retract-empty`). Both halves of that pair ran on
the same host against the same image, so the pair isolates store size on its own
terms; the empty-store half is from an earlier session than the seeded figure,
which the evidence doc records explicitly; it is not compared against the whole-repository rows above, which were
measured on a different host and a different store. The seven probes that guard
those deletes cost 0.31s and do not grow with the store
(`ledger:5998-delta-per-label-probe-seeded`,
`ledger:5998-delta-per-label-probe-empty`). This path runs on every incremental
sync, not once per generation.

An earlier row for the same shape, `ledger:5998-delta-per-label-retract-seeded`,
recorded 48.223s / 45.526s and did not reproduce. It is superseded by the rerun
above and kept only because the ledger is append-only. Do not cite it.

The large store held zero `Rationale` nodes and zero `EXPLAINS` edges, so every
`DELETE` above removed nothing. What the rows isolate:

- **The `DELETE` clause, not the `MATCH`.** Row 4 is row 1's identical
  `MATCH`/`WHERE` run as a bounded read, and stays cheap on the large store
  (ledger:5998-explains-existence-probe-read) while row 1's `DELETE` on the same
  selection does not (ledger:5998-zero-row-explains-delete-large-store). Row 4
  was measured before the guard's projection changed from `RETURN rel` to
  `RETURN true` (#5998 review F11); the shipped guard was not separately timed,
  but `RETURN true LIMIT 1` returns a literal instead of serializing a
  relationship over Bolt, so it is strictly cheaper than row 4 and row 4 bounds
  it from above.
- **Not the predicates.** Row 2 drops both property predicates and is marginally
  faster. This is *not* the [#3624](https://github.com/eshu-hq/eshu/issues/3624)
  index-defeat shape, where the predicates were the problem — here the worst
  cell has none.
- **Anchoring the source label is worth roughly 10x.** Row 3 leaves both
  endpoints untyped and costs an order of magnitude more than the
  label-anchored row 2 (ledger:5998-zero-row-explains-delete-untyped-both-ends).
- **Store size is the variable.** Rows 1, 2 and 4 each collapse to the same
  cheap cost on an empty store (`ledger:5998-zero-row-explains-delete-empty-store`,
  the empty-store trials in `ledger:5998-zero-row-explains-delete-source-anchored`'s
  note, and `ledger:5998-explains-existence-probe-read`, which measured both
  stores). Row 3 has no empty-store control in the ledger, so it is not claimed
  here: it establishes the source-anchoring cost against row 2 on the same
  store, which is all it is cited for.

### What to do

Guard a routine retract behind a bounded existence read when it can legitimately
match nothing. `ProbeExecutor` in `go/internal/storage/cypher` exists for this;
the rationale `EXPLAINS` retract uses it. The probe must mirror the retract's
`MATCH`/`WHERE` exactly, or it answers a different question than the delete it
guards.

Fail safe toward deleting. If the probe cannot run or errors, run the delete: a
redundant delete is only slow, whereas a skipped one leaves stale edges behind
permanently.

Treat the guard as temporary. When
[orneryd/NornicDB#296](https://github.com/orneryd/NornicDB/issues/296) is fixed
and the pin moves past it, a probe in front of a retract becomes a redundant
round trip rather than a saving, and the guards added for this reason can be
removed together.

This guard covers only the rationale `EXPLAINS` retract. `canonical_retract.go`'s
code-call retracts, `edge_writer_sql.go`, `canonical_inheritance_retract.go`,
`canonical_documentation_edges.go`, `canonical_codeowners_edges.go`,
`canonical_submodule_edges.go`, and `canonical_deployable_unit_edges.go` build
the same label-anchored `MATCH ... DELETE rel` shape and none of them probe
before deleting. `edge_writer_shell_exec.go` belongs on the list too, and is
worth a closer look if anyone guards these: its two retracts anchor the TARGET
label but leave the source endpoint untyped (`MATCH ()-[rel:EXECUTES_SHELL]->(target)`),
and the table above shows leaving an endpoint untyped costing an order of
magnitude more than anchoring it.

They are known-unguarded; guarding them was not in scope for the rationale
change. None of them were measured — the numbers above cover the `EXPLAINS`
shape only, and the closest ledger row for the generic shape is
`ledger:5998-zero-row-explains-delete-source-anchored`. Measure before guarding
rather than assuming the rationale numbers transfer.

### Why a small corpus cannot catch this

A 20-repo golden corpus is far too small for a store-size-proportional cost to
appear, so this class of regression passes replay gates green. Cost that scales
with the store needs a store-scale measurement, not a fixture.
