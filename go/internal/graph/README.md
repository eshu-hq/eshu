# Graph

## Purpose

`graph` owns the source-local graph write contract and the Cypher builders
used by backend adapters and schema bootstrap. It defines the `Writer` port,
the `Materialization` and `Record` input types, canonical entity merge
builders, batched UNWIND helpers, file and repository deletion mutations, and
the `EnsureSchema` constraint and index contract for both Neo4j and NornicDB
dialects.

`CypherStatement` and `CypherExecutor` live here rather than in
`internal/storage/cypher` to avoid an import cycle between the two packages.

## Where this fits

```mermaid
flowchart LR
  A["internal/projector\ncanonical_builder.go"] --> B["graph.Writer\ngraph.Materialization"]
  B --> C["storage/cypher\nCanonicalNodeWriter"]
  D["cmd/bootstrap-data-plane\ncmd/eshu local_graph_bootstrap"] --> E["graph.EnsureSchemaWithBackend*"]
  E --> F["CypherExecutor\n(Neo4j or NornicDB driver)"]
  G["storage/cypher\nwriter.go"] --> H["graph.BatchMergeEntities\ngraph.BatchMergeFiles\ngraph.BatchMergeRelationships"]
```

## Internal structure

```
graph/
  writer.go      — Writer, Materialization, Record, Result, MemoryWriter
  cypher.go      — CypherStatement, CypherExecutor
  entity.go      — EntityProps, BuildEntityMergeStatement, MergeEntity, validators
  batch.go       — BatchEntityRow, BatchFileRow, BatchRelationshipRow, batch helpers
  mutations.go   — DeleteFileFromGraph, DeleteRepositoryFromGraph, ResetRepositorySubtreeInGraph
  schema.go      — SchemaBackend, EnsureSchema, EnsureSchemaWithBackend
                   and EnsureSchemaWithBackendStrict
  schema_application.go — schema fingerprint and compatibility policy helpers
  schema_execution.go — schema DDL progress logging and context-budget handling
  schema_statements.go — ordered schema statement inspection helpers
  schema_labels.go — schema label naming helpers
```

## Ownership boundary

`graph` owns the write contract, entity merge builders, UNWIND helpers,
deletion mutations, and the schema DDL contract. It does not own backend
drivers, connection pooling, or telemetry instrumentation. Those live in
`internal/storage/cypher`, `internal/storage/neo4j`, and their NornicDB
equivalents. Backend dialect differences belong only in the schema dialect
helpers (`schemaDialectForBackend`, `nornicDBSchemaConstraint`).

## Exported surface

### Write contract

- `Writer` — narrow interface: `Write(context.Context, Materialization) (Result, error)`.
- `Materialization` — source-local write payload: `ScopeID`, `GenerationID`,
  `SourceSystem`, `Records`. `Materialization.ScopeGenerationKey()` returns a
  durable boundary string.
- `Record` — one write candidate: `RecordID`, `Kind`, `Attributes`, `Deleted`.
  `Record.Clone()` and `Materialization.Clone()` produce copy-safe values.
- `Result` — write summary: `ScopeID`, `GenerationID`, `RecordCount`,
  `DeletedCount`.
- `MemoryWriter` — in-memory `Writer` for tests and adapters.

### Cypher seam

- `CypherStatement` — one executable statement: `Cypher` string,
  `Parameters map[string]any`.
- `CypherExecutor` — interface: `ExecuteCypher(context.Context, CypherStatement) error`.

### Entity merges

- `EntityProps` — merge inputs: `Label`, `FilePath`, `Name`, `LineNumber`,
  `UID`, `Extra`.
- `ValidateCypherLabel(label string) error` — rejects labels outside the
  safe pattern.
- `ValidateCypherPropertyKeys(keys []string) error` — rejects keys with
  unsafe characters.
- `BuildEntityMergeStatement(props EntityProps) (CypherStatement, error)` —
  builds a MERGE by `uid` when `UID` is set, otherwise by
  `(name, path, line_number)`.
- `MergeEntity(ctx, executor, props)` — executes one entity merge.

### Batch UNWIND

- `DefaultBatchSize` = 500.
- `BatchEntityRow`, `BatchFileRow`, `BatchRelationshipRow` — row types for
  batch writes.
- `BatchMergeEntities(ctx, executor, label, rows, batchSize)` — splits rows
  into UID-identity and name-identity groups and merges each group in
  `batchSize`-row chunks.
- `BatchMergeFiles(ctx, executor, rows, batchSize)` — batch-merges
  `File` nodes.
- `BatchMergeRelationships(ctx, executor, rows, batchSize)` — batch-merges
  relationships. All rows must share source label, target label, and
  relationship type.

### Mutations

- `DeleteFileFromGraph(ctx, executor, filePath)` — deletes a file node and
  its contained entities; prunes orphaned parent directories in a second
  statement.
- `DeleteRepositoryFromGraph(ctx, executor, repoIdentifier) (bool, error)` —
  removes the `Repository` node and its entire owned subtree.
- `ResetRepositorySubtreeInGraph(ctx, executor, repoIdentifier) (bool, error)` —
  deletes the owned subtree while preserving the `Repository` node itself.

### Schema

- `SchemaBackend` — string enum: `SchemaBackendNeo4j`, `SchemaBackendNornicDB`.
- `SchemaStatements() []string` — returns the ordered Neo4j DDL statements
  without executing them; useful for inspection.
- `SchemaStatementsForBackend(backend SchemaBackend) ([]string, error)` —
  returns the dialect-specific ordered DDL statements.
- `SchemaApplicationForBackend(backend SchemaBackend) (SchemaApplication, error)` —
  returns the backend fingerprint, statement count, and explicit compatible
  fingerprints that graph-writing runtimes check before startup.
- `SchemaApplication` — durable schema marker payload written after successful
  bootstrap.
- `EnsureSchema(ctx, executor, logger)` — creates constraints and indexes for
  the Neo4j backend. Individual failures are logged as warnings and do not
  abort the remaining statements.
- `EnsureSchemaWithBackend(ctx, executor, logger, backend)` — same, but
  routes through the selected backend dialect.
- `EnsureSchemaWithBackendStrict(ctx, executor, logger, backend)` — same
  backend dialect routing, but returns an error when any non-context DDL
  statement fails. Deployment schema bootstrap uses this variant before writing
  the durable graph schema marker.
- `SourceLocalRecord` receives a `(scope_id, generation_id, record_id)`
  uniqueness constraint during schema setup; canonical source-local MERGE
  statements rely on it to avoid full label scans on large repositories.
- `File` receives both the legacy `path` identity constraint and a `uid`
  uniqueness constraint. Canonical file writes still MERGE by `path`, while
  shared code-call projection reads file endpoints by repo-scoped `uid`.
- OCI registry projection labels (`OciRegistryRepository`, `ContainerImage`,
  `ContainerImageIndex`, `ContainerImageDescriptor`, and
  `ContainerImageTagObservation`) receive `uid` constraints, and digest/tag-ref
  indexes keep deployment trace enrichment anchored on immutable image identity
  or explicit mutable tag observations.
- Package-registry projection labels (`Package`, `PackageVersion`,
  `PackageDependency`, `PackageRegistryPackage`,
  `PackageRegistryPackageVersion`, and `PackageRegistryPackageDependency`)
  receive `uid` constraints. Secondary indexes on package ecosystem, package
  normalized name, package-version parent ID, dependency package ID, and
  dependency version ID keep bounded package query surfaces from falling back to
  label scans.
- `IncidentRoutingEvidence` receives a `uid` constraint and the matching
  NornicDB lookup index. Reducer-owned PagerDuty routing graph writes match
  incident and routing evidence nodes by deterministic UID and never use this
  label for service, runtime, image, commit, pull-request, Jira, or root-cause
  truth.
- `ExternalPrincipal` receives a `uid` constraint and the matching NornicDB
  lookup index. Reducer-owned S3 external-principal grant writes key the node by
  bounded principal kind/value and connect existing S3 `CloudResource` nodes to
  those identities through `GRANTS_ACCESS_TO` edges.

See `doc.go` for the godoc contract.

## Dependencies

Standard library (`context`, `crypto/sha256`, `encoding/hex`, `fmt`,
`log/slog`, `regexp`, `strings`). No internal-package imports.
`CypherStatement` and `CypherExecutor` duplicate their `storage/cypher`
counterparts by design to avoid a cycle.

## Telemetry

`EnsureSchema`, `EnsureSchemaWithBackend`, and
`EnsureSchemaWithBackendStrict` log every DDL statement before and after
execution via `slog`. Each log includes `graph_backend`, `schema_phase`,
`statement_index`, `statement_total`, `schema_statement`, and `duration_ms` on
completion. Generic DDL failures still log as warnings and continue for
non-strict callers so idempotent already-exists or optional full-text behavior
does not block startup. Strict callers return an error after the ordered schema
attempt completes if any non-context statement failed. Context deadline or
cancellation errors fail fast because the caller has already lost its execution
budget. No metrics or span instruments are registered here; backend executors
own those.

No-Regression Evidence: focused schema tests keep the prior non-deadline warning
behavior while adding a deadline regression that proves context budget
exhaustion is returned after the first failed statement.

Observability Evidence: structured schema statement logs expose backend, phase,
ordinal, total, duration, bounded statement summary, and failure class so a
Kubernetes bootstrap job no longer appears hung after Postgres schema completes.

No-Regression Evidence: #2902 adds
`TestSchemaApplicationsDeclareCompatibilityDecision`, which pins the
current Neo4j fingerprint
`3a34d8460063f6d6e390dbea3bdacd1ecf0f2e9ff8b92bbea0b7382f1fdf2246`
(213 statements) and NornicDB fingerprint
`2e29b77ef4364aa4653ad1d6398cee136e3c4c099e2f2eb157eae38a1f10b377`
(275 statements). The latest DDL bump adds only `Function.repo_id` and
`Function.path` lookup indexes, so older writers remain compatible while
reducer-owned Function edge retractions can avoid large label scans.

No-Observability-Change: graph schema compatibility remains a Postgres marker
read/write contract through `graphschemacompat`; this update changes only the
compiled compatibility list returned with the current schema application. Schema
DDL execution still emits the existing structured statement logs, and runtime
startup refusal/acceptance continues through the existing caller logs and
Postgres query instrumentation.

## Gotchas / invariants

- `cypherSafePattern` (`entity.go:12`) accepts `[a-zA-Z_][a-zA-Z0-9_]*` only.
  Callers passing dynamic label or property-key strings must call
  `ValidateCypherLabel` or `ValidateCypherPropertyKeys` before building a
  statement; the builders return errors if validation fails.
- `BatchMergeEntities` splits rows into UID-identity and name-identity groups
  (`batch.go:102`) so each MERGE clause can hit a graph index directly. All
  rows in a single call must share the same `Label`.
- `BatchMergeRelationships` reads `SourceLabel`, `TargetLabel`, and `RelType`
  from the first row (`batch.go:208`) and requires every subsequent row to
  match. Mixed-type rows must be split into separate calls.
- `Module` nodes use `CREATE INDEX` not a uniqueness constraint (`schema.go:57`)
  because canonical import-graph writes MERGE on the globally shared `name`
  property while semantic entity writes MERGE on the per-repo `uid`. A global
  name uniqueness constraint causes `ConstraintValidationFailed` when multiple
  repositories share module names like `consts` or `index`.
- `NornicDB` composite `IS UNIQUE` constraints are dropped by
  `nornicDBSchemaConstraint` because NornicDB's parser rejects the
  multi-property form. For code-entity identities such as `Function` and
  `Class`, the projector derives `uid` from the same `(repo, path, type, name,
  line)` tuple before graph write, and NornicDB enforces the generated `uid`
  constraint plus lookup index. Neo4j keeps the direct composite constraint.
- `DeleteFileFromGraph` runs two sequential `ExecuteCypher` calls
  (`mutations.go:29`, `:41`). If the second call fails, orphaned directories
  may remain until the next deletion or schema repair.
- `ResetRepositorySubtreeInGraph` preserves the `Repository` node;
  `DeleteRepositoryFromGraph` removes it. Choosing the wrong one during
  re-ingestion will leave a stale or missing root node.
- The schema contract is the checked-in Go-owned truth for node labels,
  constraints, performance indexes, and full-text indexes. Changes here must
  update the active ADR chunk status row.
- Additive rolling-upgrade compatibility is explicit: a newer
  `SchemaApplication` may list older writer fingerprints as compatible. A
  destructive schema change, or an additive schema change coupled to a new
  reducer domain older workers cannot parse, must leave the compatibility list
  empty so stale graph writers refuse before they write. Any schema fingerprint
  change must update `TestSchemaApplicationsDeclareCompatibilityDecision`
  and either add safe predecessor fingerprints or deliberately leave
  compatibility empty with a documented atomic-rollout reason.
- Terraform schema labels include resource/config entities plus backend,
  import, moved, removed, check, and lockfile-provider evidence. Keep that list
  aligned with `internal/content/shape` and `internal/storage/cypher`.

## Related docs

- `docs/public/architecture.md` — ownership table
- `docs/public/reference/backend-conformance.md` — NornicDB
  compatibility dialect evidence
- `go/internal/storage/cypher/README.md` — canonical write adapters that
  implement `Writer` and use the batch/mutation helpers
