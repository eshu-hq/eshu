# AGENTS.md — storage/cypher guidance for LLM assistants

## Read first

1. `go/internal/storage/cypher/README.md` — pipeline position, executor chain,
   exported surface, and operational notes
2. `go/internal/storage/cypher/writer.go` — `Executor` interface, `Statement`,
   `Plan`, `GroupExecutor`, `PhaseGroupExecutor`, `Adapter`; the full contract
   before touching anything else
3. `go/internal/storage/cypher/canonical_node_writer.go` — `CanonicalNodeWriter.Write`,
   phase dispatch, and the GroupExecutor/PhaseGroupExecutor/sequential paths
4. `go/internal/storage/cypher/retrying_executor.go` — NornicDB MERGE unique
   conflict handling and the shared retry loop for both `Execute` and
   `ExecuteGroup`
5. `go/internal/telemetry/instruments.go` and `contract.go` — metric names and
   span constants before adding new telemetry

## Invariants this package enforces

- **Idempotency** — every write path uses MERGE or ON CONFLICT semantics; no
  unconditional CREATE. `doc.go` states this as a package invariant.
- **Phase order** — `CanonicalNodeWriter.Write` phases run strictly in order:
  retract → repository_cleanup → repository → directories → files → entities →
  entity_retract → entity_containment → terraform_state → oci_registry →
  package_registry_packages → package_registry_versions →
  package_registry_dependency_targets → package_registry_dependencies →
  modules → structural_edges → package_registry_version_edges →
  package_registry_dependency_edges. Parent nodes must exist before child MATCH
  statements run, repository cleanup must commit before the repository MERGE,
  and stale entity cleanup must run after current entity upserts so it can avoid
  giant `uid IN` exclusion filters. The two `package_registry_*_edges` phases run
  LAST, after every package_registry node phase, because they MATCH the
  multi-label `Package`/`PackageVersion`/`PackageDependency` nodes those node
  phases create.
- **package_registry edges dispatch in a SECOND ExecuteGroup** — on the atomic
  `GroupExecutor` projector path, `CanonicalNodeWriter.Write` partitions the
  two deferred `package_registry_*_edges` phases out of the main group and
  dispatches them as a separate, second `ExecuteGroup` that runs only after the
  node group commits. This is required for NornicDB read-your-writes: a node
  MERGE'd with multiple labels in one statement is invisible to a later
  same-transaction `UNWIND $rows … MATCH` against one of those labels, so an
  inline edge MATCH+MERGE in the same atomic transaction finds nothing and the
  `HAS_VERSION`/`DECLARES_DEPENDENCY`/`DEPENDS_ON_PACKAGE` edges never
  materialize. Deferring the edges to a second committed-node group fixes this.
  The phase-group and sequential paths need no special handling because they
  already commit per phase and the edge phases run last. This deferral is
  MULTI-LABEL specific: single-label edges (the inline `File` edges and the
  `directory_edges` phase, `Directory -> Directory CONTAINS`) get cross-statement
  read-your-writes within one atomic group and stay inline in the main group.
- **directory writes are split into a node phase and an edge phase** — the
  `directories` phase MERGEs every `Directory` by path with NO parent MATCH;
  the `directory_edges` phase (which runs after it) wires each directory to its
  parent (Repository for depth-0, parent Directory for depth-N). The split is
  required for the phase-group executor, which runs each phase as one transaction
  with NO read-your-writes for a later statement's `MATCH` against an earlier
  statement's MERGE in the same phase — so a depth-N directory whose parent was
  MERGE'd in the same combined phase found nothing and silently dropped every
  file and entity beneath it (#4019). Splitting the parent edge into its own
  later-committing phase resolves it; do not recombine node creation and parent
  edges into one phase.
- **No GraphWrite type** — this package does not export a GraphWrite port.
  The backend seam is `Executor`. Every caller in `internal/projector` and
  `internal/reducer` uses the projector CanonicalWriter or
  SharedProjectionEdgeWriter interfaces that are backed by `CanonicalNodeWriter`
  and `EdgeWriter`.
- **No direct driver calls in this package** — the concrete Neo4j and NornicDB
  driver sessions live in `cmd/` wiring. This package only defines contracts.
- **RetryingExecutor.ExecuteGroup retries on MERGE-shaped groups** — both
  `Execute` and `ExecuteGroup` run through `runWithRetry` with the same
  exponential-backoff cadence. `ExecuteGroup` retries on commit-time UNIQUE
  conflicts only when every statement in the group contains MERGE
  (`allStatementsAreMerge`); mixed groups containing non-MERGE statements
  are NOT retried, preserving idempotency safety. Driver-level
  `session.ExecuteWrite` retries handle Neo.TransientError.* codes; the Eshu
  retry loop additionally handles driver `ConnectivityError` only when the
  driver classifies it as retryable. A `ConnectivityError` wrapping
  `CommitFailedDeadError` is not retried in place because the commit outcome is
  unknown. A durable caller may later replay still-pending idempotent work after
  backoff. The loop also handles
  Neo.ClientError.Transaction.TransactionCommitFailed when classified as a
  commit-time UNIQUE conflict (`retrying_executor.go`).
- **CanonicalNodeWriter.Write wraps escaping errors as retryable** — every
  return path in `CanonicalNodeWriter.Write` (atomic group, phase group, and
  sequential) routes its error through `WrapRetryableNeo4jError`, matching every
  other graph writer in this package (`edge_writer.go`, `cloud_resource_node_writer.go`,
  the EC2/IAM/S3 writers, `semantic_entity.go`). Without this, transient NornicDB
  failures (driver retry-budget exhaustion `*TransactionExecutionLimit`,
  `*ConnectivityError`, and the codes in `retryableNeo4jCodes`) reach the
  projector queue as a non-`projector.RetryableError` and dead-letter at
  `internal/storage/postgres/projector_queue.go` instead of requeuing with
  backpressure. This does NOT loosen classification: `WrapRetryableNeo4jError`
  only wraps the known transient set, so terminal errors (schema constraint
  violations, syntax) are returned unchanged and stay terminal. Do not strip
  this wrapping.
- **OperationCanonicalUpsert vs. OperationUpsertNode** — canonical domain nodes
  use `OperationCanonicalUpsert`; source-local `SourceLocalRecord` writes use
  `OperationUpsertNode`/`OperationDeleteNode`. Do not mix them.
- **OCI tags are weak evidence** — `oci_registry_canonical_writer.go` writes
  manifests and indexes on `ContainerImage` labels keyed by digest-backed uid.
  Tag observations are separate `ContainerImageTagObservation` nodes; do not
  MERGE image manifest or index identity from tag text.
- **Package source hints are weak evidence** —
  `package_registry_canonical_writer.go` writes package identity, package
  version identity, and package-native dependency identity (the node-only
  upserts). The `HAS_VERSION`, `DECLARES_DEPENDENCY`, and `DEPENDS_ON_PACKAGE`
  edge writers live in `package_registry_edge_writer.go` and run as the deferred
  second ExecuteGroup (see the phase-order invariant above). Do not join to
  `Repository` or create ownership/publication edges from registry source URLs.
- **Identity cleanup** — repository upserts must keep cleanup before MERGE and
  in a separate phase group for non-first-generation scopes. First-generation
  scopes skip repository cleanup because there is no prior repository identity
  for that source-local scope. Directory and File writers must not restore
  current-directory or current-file `DETACH DELETE` cleanup.
- **Entity cleanup anchors** — stale entity retractions and current-generation
  `Class`/`Function` containment cleanup must use label-specific anchors. Do
  not collapse those statements back into unlabelled `MATCH (n)` or UID MATCH
  shapes; they are correct Cypher but can force broad graph scans on local
  NornicDB. Stale entity retractions belong in the `entity_retract` phase after
  current entity upserts, not in the pre-upsert `retract` phase.
- **Directory and File nodes update in place** — do not replace current
  `Directory` or `File` nodes just to avoid stale edges. Local NornicDB pays
  heavily for `DETACH DELETE` on those identities. File paths update with
  `MATCH (f:File {path: row.path})`; missing files use a `WHERE NOT EXISTS`
  guard before MERGE so existing `File.path` rows avoid the MERGE
  unique-conflict path.
- **Code-call logs need route clues** — code-call edge statements should keep
  bounded summaries with relationship type, source label, target label, and row
  count. Do not add file paths, entity IDs, or symbols to metric labels or
  shared-edge summaries.
- **Inheritance provenance is row-derived** — INHERITS, IMPLEMENTS, OVERRIDES,
  and ALIASES edge statements keep the same `UNWIND` / label+`uid` `MATCH` /
  relationship `MERGE` shape while reading `confidence`, `reason`, and
  `resolution_method` from the row. Do not reintroduce relationship-local
  `confidence = 0.95` literals in inheritance edge writers.

No-Regression Evidence: `go test ./internal/reducer -run 'TestExtractInheritanceRowsStampsDeclaredResolutionMethod' -count=1` and `go test ./internal/storage/cypher -run 'TestBuildInheritanceRowMap(DerivesTieredConfidence|DefaultsToLegacyConfidence)|TestInheritanceCypherTemplatesAreParameterized|TestBuildInheritanceRowMapRoutesImplements|TestEdgeWriterWriteEdgesInheritanceDispatch' -count=1` prove inheritance and IMPLEMENTS rows carry `codeprovenance` methods, derive confidence/reason from the row, and preserve the existing backend-neutral `UNWIND` plus label/uid `MATCH` plus relationship `MERGE` shape for one-row inheritance and IMPLEMENTS inputs.

No-Observability-Change: inheritance edge writes still flow through `EdgeWriter.WriteEdges`, existing route summaries, `GroupExecutor`/sequential execution, shared-edge group metrics, statement summaries, retry wrapping, and failure logs. This change adds no metric name, metric label, worker, queue domain, runtime knob, backend branch, or new graph-write route.

## Common changes and how to scope them

- **Add a new canonical domain node type** → add a BuildCanonical...Upsert
  function (follow the pattern of `BuildCanonicalWorkloadUpsert`) in
  `canonical.go` or a new file; add a BuildRetract... companion following
  `BuildRetractRepoDependencyEdges` in `canonical_retract.go`; add unit tests in
  `canonical_test.go`. No writer changes needed — callers build the `Statement`
  and pass it to any `Executor`.

- **Add a new shared projection domain (EdgeWriter)** → add the domain constant
  in `internal/reducer`; add a `batchCypherForDomain` case and a `buildRowMap`
  case in `edge_writer.go`; add tests in `edge_writer_test.go`. Verify the new
  UNWIND Cypher template against both Neo4j and NornicDB if both backends are
  active.

- **Change SQL relationship writes** → update `edge_writer_sql.go`,
  `canonical.go`, and the SQL retraction tests together. `EXECUTES` is a
  reachability edge from `SqlTrigger` to `SqlFunction`; removing it from either
  the write path or `BuildRetractSQLRelationshipEdgeStatements` can make
  trigger-bound stored routines appear dead.

- **Add a new executor wrapper** → implement `Executor`; optionally implement
  `GroupExecutor` and/or `PhaseGroupExecutor`; add unit tests. Wire in `cmd/`
  only, not here.

- **Tune batch sizes for a backend** → use `CanonicalNodeWriter` builder
  options (`WithEntityBatchSize`, `WithEntityLabelBatchSize`, etc.) in `cmd/`
  wiring. Do not hard-code backend-specific values inside `canonical_node_writer.go`.

- **Add telemetry for a new metric** → add the instrument to
  `go/internal/telemetry/instruments.go`; add the metric name string to the
  compile-time list in `go/internal/telemetry/contract.go`; record via
  `Instruments.*` in the write path.

Performance Evidence: Remote full-corpus NornicDB Compose proof on the
code-call partition-loading branch reached bootstrap completion with schema
applied and active code-call partition leases, but tiny `INSTANTIATES` code-call
groups repeatedly spent about 25-28 s in the graph write while adjacent typed
`CALLS` and `REFERENCES` groups completed in milliseconds. The slow route used
the multi-label `Function|Class|File` to `Class|Struct|Enum` template for
1-17-row groups; exact endpoint labels were present in the row payload. The
fix keeps the same backend-neutral `UNWIND` plus `MERGE` semantics while routing
typed `INSTANTIATES` rows through exact label + `uid` matches. Red evidence:
`go test ./internal/storage/cypher -run
'TestEdgeWriterWriteEdgesInstantiatesUsesExactEndpointLabels' -count=1` failed
because the old template still used multi-label endpoint matches. Green
evidence: `go test ./internal/storage/cypher -run
'TestEdgeWriterWriteEdgesInstantiatesUsesExactEndpointLabels|TestBuildCodeCallRowMapRoutesInstantiates'
-count=1` and `go test ./internal/storage/cypher -count=1`.

No-Observability-Change: typed `INSTANTIATES` rows still execute through
`EdgeWriter.WriteEdges`, existing grouped/sequential executor paths,
`CodeCallEdgeDuration`, `CodeCallEdgeBatches`, shared edge group metrics, retry
wrapping, and bounded route summaries with relationship, source label, target
label, and row count. The change adds no metric name, metric label, worker,
queue domain, runtime knob, backend branch, or new graph-write route.

No-Regression Evidence: package-registry dependency-target package rows now
skip UIDs already covered by primary package rows in the same canonical
materialization. The baseline failed
`go test ./internal/storage/cypher -run
'TestCanonicalNodeWriterSkipsDependencyTargetsCoveredByPackageRows' -count=1`
with one duplicate target statement for a one-package, one-dependency NPM input
shape. After the fix, `go test ./internal/storage/cypher -run
'TestCanonicalNodeWriter(SkipsDependencyTargetsCoveredByPackageRows|DeduplicatesPackageRegistryDependencyTargets|DeduplicatesPackageRegistryPackages|DeduplicatesPackageRegistryPackagesWithDeterministicTieBreaker|BuildsPackageRegistryStatements|SeparatesPackageRegistryPhaseGroups)'
-count=1` and `go test ./internal/projector ./internal/storage/cypher -count=1`
pass. The backend contract is unchanged: primary package rows still write first,
target-only dependencies still create target packages, and terminal row counts
drop only for duplicate same-UID target upserts.

No-Observability-Change: package-registry dependency-target filtering runs
inside statement construction before the existing `CanonicalNodeWriter.Write`
path. Existing canonical write spans, phase metadata, package identity locks,
retry classification, backend query metrics, and phase failure logs still cover
the write. The change adds no metric name, metric label, worker, queue domain,
runtime knob, backend branch, or new graph-write route.

## CodeTaintEvidence writer (value-flow projection, #2889)

`CodeTaintEvidenceWriter` upserts value-flow taint findings as `CodeTaintEvidence`
nodes attached to their `Function`. It is the reducer-owned graph-write for the
`code_taint_evidence` projection domain.

No-Regression Evidence: `go test ./internal/storage/cypher -run 'TestCodeTaintEvidenceWriter' -count=1`
and `go test ./internal/graph -run 'TestSchemaStatements.*CodeTaintEvidence' -count=1`
prove the writer emits one batched, backend-neutral `UNWIND $rows` statement that
`MATCH (f:Function {uid})` (never `MERGE`, so a missing Function adds no orphan
node), `MERGE (ev:CodeTaintEvidence {uid})`, and
`MERGE (f)-[:HAS_TAINT_EVIDENCE]->(ev)`, and that retraction is a single
`DETACH DELETE` scoped to `scope_id` + `evidence_source`. Input shape: one row per
resolved finding per scope generation (bounded by `DefaultBatchSize`); the
MERGE-on-uid identity plus the new `code_taint_evidence_uid_unique` constraint
(NornicDB: a uid lookup index) keep the upsert O(rows) and idempotent. Backend:
NornicDB default and Neo4j compatibility, same `Executor`/`GroupExecutor` path as
every other evidence writer. The domain is gated off until its loader and writer
are wired in `cmd/reducer`, so there is no production graph-write load on this
path yet.

No-Observability-Change: the writer flows through the existing `Executor`/
`GroupExecutor` dispatch, `Statement` phase/label/summary metadata
(`code_taint_evidence` phase, `CodeTaintEvidence` label), retry wrapping
(`WrapRetryableNeo4jError`), and failure logging. It adds no new metric name,
metric label, worker, queue domain, runtime knob, backend branch, or other new
graph-write route surface.

## CodeInterprocEvidence writer (cross-function value-flow projection, #2906)

`CodeInterprocEvidenceWriter` upserts cross-function value-flow findings as
`TAINT_FLOWS_TO` edges between the source and sink `Function` nodes. It is the
reducer-owned graph-write for the `code_interproc_evidence` projection domain and
mirrors the existing reducer-owned scoped edges (`iam_can_assume`,
`handles_route`): the flow is an edge, not a node, so it needs no new schema
constraint or index.

No-Regression Evidence: `go test ./internal/storage/cypher -run 'TestCodeInterprocEvidenceWriter' -count=1`
proves the writer emits one batched, backend-neutral `UNWIND $rows` statement that
`MATCH (s:Function {uid})` and `MATCH (t:Function {uid})` (never `MERGE`, so a
finding whose endpoint is absent draws no edge to a phantom node),
`MERGE (s)-[rel:TAINT_FLOWS_TO {evidence_uid: row.uid}]->(t)`, and that retraction
is a single `DELETE` scoped to `scope_id` + `evidence_source`. Input shape: one
row per resolved cross-function finding per scope generation (bounded by
`DefaultBatchSize`); the MERGE-on-`evidence_uid` identity keeps the upsert
O(rows) and idempotent, and the MERGE is bounded by the two MATCHed endpoints so
it needs no global relationship index. Backend: NornicDB default and Neo4j
compatibility, same `Executor`/`GroupExecutor` path as every other evidence
writer. The domain is gated off until its loader and writer are wired in
`cmd/reducer`, so there is no production graph-write load on this path yet.

No-Observability-Change: the writer flows through the existing `Executor`/
`GroupExecutor` dispatch, `Statement` phase/label/summary metadata
(`code_interproc_evidence` phase, `TAINT_FLOWS_TO` label), retry wrapping
(`WrapRetryableNeo4jError`), and failure logging. It adds no new metric name,
metric label, worker, queue domain, runtime knob, backend branch, or other new
graph-write route surface.

## Failure modes and how to debug

- Symptom: `eshu_dp_neo4j_deadlock_retries_total` rising → likely causes:
  concurrent MERGE on shared nodes (Repository, Directory), transient driver
  connectivity failures, or retryable NornicDB commit conflicts → check the
  paired `neo4j transient error, retrying` logs before changing worker
  concurrency; `RetryingExecutor.MaxRetries` is 3 by default and raising it
  extends recovery time.

- Symptom: `eshu_dp_canonical_phase_duration_seconds{phase="retract"}` elevated
  → likely cause: large stale node set or missing index on `repo_id +
  evidence_source + generation_id` → check graph schema; retract Cypher uses
  DETACH DELETE which is proportional to edge volume.

- Symptom: `failure_class=graph_write_timeout` in queue rows → likely cause:
  `TimeoutExecutor.Timeout` too short for current write volume → check
  `TimeoutHint` in the error for the env var to adjust; check
  `eshu_dp_canonical_phase_duration_seconds` for the slow phase. If the slow
  statement is source-local entity cleanup, verify it is anchored to the
  concrete entity label before raising timeouts.

- Symptom: `eshu_dp_canonical_atomic_fallbacks_total` > 0 → the executor does
  not implement `GroupExecutor`; writes are sequential; investigate whether
  the wired executor is missing `ExecuteGroup`.

- Symptom: NornicDB MERGE unique constraint violation not retried → check
  `isNornicDBMergeUniqueConflict` in `retrying_executor.go:129`; the cypher
  string must contain MERGE and the error must match the expected message shape.

## Anti-patterns

- **Do not add `if backend == "nornicdb"` branches** in writers, statement
  builders, or callers. Backend dialect differences belong only in documented
  narrow seams (schema adapters, `cmd/` wiring, executor constructors).
- **Do not call Neo4j or NornicDB drivers directly** from inside this package.
  Concrete driver sessions belong in `cmd/` wiring.
- **Do not change `Executor` interface signature** without coordinating all
  `cmd/` wiring sites and the projector CanonicalWriter contract.
- **Do not use CREATE instead of MERGE** in canonical Cypher templates. CREATE
  breaks idempotency and will cause duplicate-node errors on retries.
- **Do not add `GroupExecutor` to `ExecuteOnlyExecutor`**. It intentionally
  hides the group path so callers during concurrent ingestion do not hold large
  atomic transactions.
- **Do not write an `UNWIND $rows AS row MATCH (n:Label {key: row.key}) SET
  ...` statement with no `MERGE` anywhere in it.** Issue #5652: this shape
  silently drops its `SET` on the pinned production NornicDB v1.1.11 image —
  the statement reports success but the property is never persisted.
  `unwind_bare_match_set_gate_test.go` fails the build if this shape
  reappears. If the writer must never fabricate a node, do not blindly switch
  to `MERGE` either — follow the `posture_node_existence.go` pattern: read
  which candidate identities already exist via a separate query first, drop
  unconfirmed rows in Go, then `MERGE` only the confirmed subset.

## What NOT to change without an ADR

- `Executor` interface shape — changes break every `cmd/` wiring site and the
  projector CanonicalWriter contract; see
  `docs/public/reference/backend-conformance.md`.
- `CanonicalNodeWriter` phase order — phase ordering is a correctness constraint
  because later phases MATCH nodes created by earlier phases; see
  `docs/public/reference/cypher-performance.md`.
- Retraction Cypher label sets — adding or removing node labels from retract
  queries requires coordinated graph schema migration.
- `RetryingExecutor` retry classification logic — NornicDB compatibility
  behavior is documented in `retrying_executor.go`; changes must be evidence-
  backed per the NornicDB maintainer patch bar.

## Evidence and change history

The dated evidence log for this package is in
[AGENTS-evidence-history.md](AGENTS-evidence-history.md), split out to keep this
read-first file under the repository's 500-line cap. It preserves every prior
entry: canonical-writer retryable-error propagation (#3483), the uid-anchored
`TAINT_FLOWS_TO` / `CodeTaintEvidence` retract and its NornicDB Bolt
`ExecuteWrite` dispatch route (#4893), the count-gated orphan-sweep write skip
(#4900), the UNWIND bare-MATCH SET silent write-loss fix (#5652), and later
entries. Read it before touching retract dispatch, the orphan-sweep gate, or
the NornicDB write path. Add new dated entries there, not here.
