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

## Pitfall: Every Relationship-Existence Predicate Is Mis-Evaluated

### Observed shape

On both pinned NornicDB backends (v1.1.11 and the PR261/compose-default
image), every Cypher shape that asks "does this node have any relationship"
without binding a concrete relationship variable is wrong:

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
pinned backends:

```cypher
-- BROKEN: returns zero rows on both pinned NornicDB backends, no error
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
(n)--()` predicate silently ignored, on both the pinned v1.1.11 and the
PR261/compose-default NornicDB images.

## Pitfall: `OPTIONAL MATCH` + Aggregate Collapses Every Zero-Match Group Into One Row

### Observed shape

Measured directly over the HTTP `tx/commit` endpoint and independently via
`neo4j-go-driver/v5` (both paths reproduce it) against the canonical
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

On the pinned PR261/compose-default NornicDB image, combining an inline
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

On the pinned PR261/compose-default NornicDB image, `EXISTS { MATCH (pattern)
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

On the pinned production image (`nornicdb-cpu-bge:v1.1.11`), an
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
  did not reproduce in any production dispatch mode on a fresh container. The
  discrepancy was not root-caused; the most likely mechanism is stack
  contamination (the original probe ran on the same instance as an earlier
  abandoned `UNIQUE` constraint; see the constraint drop/recreate pitfall), but
  that is unconfirmed — tracked as #5671. No fix ships and no rewrite is needed.
- `UNWIND`-batched `MATCH ... DELETE` (the retract/refresh edge-cleanup
  statements) no-ops only under the atomic `ExecuteGroup` managed-transaction
  path. Production routes these with `OperationCanonicalRetract` through
  `PhaseGroupExecutor.executeSequentialRetractPhase`, which always uses
  sequential auto-commit `Execute`, never `ExecuteGroup` — so the shape is
  production-unreachable there. If you ever route a retract through
  `ExecuteGroup`, rewrite the batch shape from `UNWIND` to `WHERE ... IN`
  (`MATCH (f:File) WHERE f.path IN $file_paths MATCH (f)-[r:IMPORTS]->(:Module)
  DELETE r`), which was proven to delete correctly. The underlying backend
  shapes are tracked upstream as #4902 and #5323.

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
