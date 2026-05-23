# AGENTS.md — internal/graph guidance for LLM assistants

## Read first

1. `go/internal/graph/README.md` — package position, exported surface,
   invariants, and schema dialect notes
2. `go/internal/graph/writer.go` — `Writer`, `Materialization`, `Record`,
   `MemoryWriter`
3. `go/internal/graph/entity.go` — `EntityProps`, `BuildEntityMergeStatement`,
   `MergeEntity`, `ValidateCypherLabel`
4. `go/internal/graph/batch.go` — `BatchMergeEntities`, `BatchMergeFiles`,
   `BatchMergeRelationships` and the UNWIND row types
5. `go/internal/graph/schema.go`, `schema_statements.go`,
   `schema_execution.go`, and `schema_labels.go` — `EnsureSchemaWithBackend`,
   `SchemaBackend`, the constraint/index lists, ordered statement inspection,
   and schema DDL observability helpers
6. `go/internal/storage/cypher/README.md` — which adapters implement `Writer`
   and use these helpers

## Invariants this package enforces

- **Cypher-safe labels and property keys** — `ValidateCypherLabel` at
  `entity.go:38` accepts `[a-zA-Z_][a-zA-Z0-9_]*`. `ValidateCypherLabel` and
  `ValidateCypherPropertyKeys` must be called on any dynamic input; the
  builders return errors otherwise.
- **BatchMergeEntities row homogeneity** — all rows passed to
  `BatchMergeEntities` must share the same `label` argument. UID-identity and
  name-identity rows are split internally at `batch.go:102`.
- **BatchMergeRelationships row homogeneity** — `SourceLabel`, `TargetLabel`,
  and `RelType` are read from `rows[0]` at `batch.go:208`. Mixed-type rows
  must be split before calling.
- **Module index not constraint** — `schema.go:57` uses `CREATE INDEX` for
  `Module` nodes. Do not convert it to a uniqueness constraint.
- **File has two stable identities** — `schema.go:36` keeps `File.path` as the
  canonical merge key, while `schema.go:147` includes `File` so shared
  code-call projection can match file endpoints by repo-scoped `uid`.
- **OCI image truth is digest-first** — `ContainerImage`, `ContainerImageIndex`,
  and `ContainerImageDescriptor` labels get `uid` constraints and digest
  indexes. `ContainerImageTagObservation` keeps a separate `image_ref` index for
  mutable tag evidence; do not use tag text as image identity.
- **Package truth is identity-first** — `Package`, `PackageVersion`,
  `PackageDependency`, `PackageRegistryPackage`,
  `PackageRegistryPackageVersion`, and `PackageRegistryPackageDependency`
  labels get `uid` constraints. Keep package ownership and repository
  publication out of schema assumptions unless reducer admission owns that
  truth.
- **NornicDB composite constraint suppression** — `nornicDBSchemaConstraint`
  drops composite `IS UNIQUE` constraints. The NornicDB dialect uses `uid`
  uniqueness constraints for the same labels instead.
- **No import cycles** — `CypherStatement` and `CypherExecutor` are defined
  here, not imported from `storage/cypher`. Do not add an import of
  `internal/storage/cypher` or any package that imports it.

## Common changes and how to scope them

- **Add a new node label to the schema** → add a constraint entry to
  `schemaConstraints` in `schema.go` and, if the label needs uid-uniqueness
  with NornicDB, add it to `uidConstraintLabels`. Run
  `go test ./internal/graph -count=1` and `go test ./internal/storage/cypher -count=1`.
  Update the active ADR chunk status row.

- **Add a new entity merge path** → if it is a single merge, use
  `BuildEntityMergeStatement` or `MergeEntity`. If it is bulk, add a
  `BatchEntityRow` slice and call `BatchMergeEntities`. Write a test in
  `entity_test.go` or `batch_test.go` first.

- **Add a new mutation** → model it after `DeleteFileFromGraph` in
  `mutations.go`. Each step should be a separate `ExecuteCypher` call with
  a descriptive error wrap. Add a test in `mutations_test.go` first.

- **Add a new backend dialect** → add a `SchemaBackend` constant, extend
  `schemaDialectForBackend` in `schema.go`, and add tests in `schema_test.go`.
  Keep dialect logic inside `schema.go`; do not branch on backend in
  `entity.go`, `batch.go`, or `mutations.go`.

## Failure modes and how to debug

- Symptom: `BuildEntityMergeStatement` returns `invalid Cypher label` error →
  cause: the `Label` field contains characters outside `[a-zA-Z_][a-zA-Z0-9_]*`
  → fix: validate the entity type string before passing it as a label.

- Symptom: `ConstraintValidationFailed` on `Module` nodes in Neo4j →
  cause: someone added a uniqueness constraint for `Module` where the index
  already exists → fix: remove the constraint; `Module` must stay as an
  index because repos share module names like `consts` or `index`
  (`schema.go:57`).

- Symptom: `EnsureSchemaWithBackend` logs warnings but returns nil →
  cause: one or more individual DDL statements failed (schema already exists
  or backend-specific parse error) → these warnings are expected on
  idempotent runs; look for genuine errors by checking the `error` field in
  the structured log output.

- Symptom: orphaned `Directory` nodes remain after `DeleteFileFromGraph` →
  cause: the prune statement at `mutations.go:41` failed → check the
  `ExecuteCypher` error return in the calling code; the operation is not atomic.

## Anti-patterns specific to this package

- **Importing `internal/storage/cypher` from here** — creates a cycle.
  `CypherStatement` and `CypherExecutor` are intentionally duplicated.

- **Backend-conditional logic in `entity.go`, `batch.go`, or `mutations.go`**
  — dialect differences belong only in `schema.go`'s dialect helpers and in
  `internal/storage/cypher` adapters.

- **Skipping `ValidateCypherLabel` on dynamic input** — unsanitized labels or
  property keys produce invalid Cypher that the backend rejects at runtime,
  usually with an opaque parse error.
