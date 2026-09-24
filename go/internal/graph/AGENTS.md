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
   `schema_execution.go`, `schema_application.go`, and `schema_labels.go` —
   `EnsureSchemaWithBackend`, `SchemaBackend`, the constraint/index lists,
   ordered statement inspection, graph schema fingerprints, compatibility
   policy, and schema DDL observability helpers
6. `go/internal/storage/cypher/README.md` — which adapters implement `Writer`
   and use these helpers

## Invariants this package enforces

- **Index keys come from the DDL** — `SchemaIndexKeys` parses every non-fulltext
  statement the schema can create on either backend and returns an error for a
  shape it cannot read; `TestSchemaIndexKeysCoverEveryIndexStatement` and
  `TestGuardIndexKeyWritesCoversEverySchemaKey` fail when a new index is not
  guarded. When you add a relationship index or a new DDL form, extend the
  parser and the guard in the same change; do not relax the tests.
- **A write shape the analyzer cannot read must be loud** —
  `UnanalyzedIndexWrites` reports schema-indexed labels written in an unread
  shape, and `TestProductionCypherLiteralsAreGuarded` fails when a production
  writer takes one. When you add a writer shape, extend the analyzer with a
  RED test first; add a `sweepAllow` entry only for a literal that writes no
  measurable value, with the reason. Do not claim the guard covers every
  possible Cypher shape.
- **Never truncate or hash an indexed value to fit** — `GuardIndexKeyWrites`
  drops the row so the graph holds no corrupted identity. `MaxIndexKeyBytes`
  is measured against the pinned Neo4j; re-measure before raising it.

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
  `PackageDependency`, `PackageArtifact`, `RegistryEvent`,
  `PackageRegistryPackage`, `PackageRegistryPackageVersion`,
  `PackageRegistryPackageDependency`, `PackageRegistryPackageArtifact`, and
  `PackageRegistryRegistryEvent` labels get `uid` constraints (#5458). The
  deferred `HAS_ARTIFACT` and `HAS_REGISTRY_EVENT` edge MATCHes both anchor on
  `uid` alone (and, on the `PackageVersion` side, also `package_id` — see
  `package_registry_artifact_writer.go` and `package_registry_event_writer.go`),
  which the `uid` constraint already backs; do not add a
  `package_artifact_version_id`/`package_artifact_package_id` or
  `registry_event_version_id`/`registry_event_package_id` index pair
  speculatively — a #5820 P2 review found no query anywhere in the repo
  filtering `PackageArtifact` by `version_id` or `package_id`, and the same
  search found none filtering `RegistryEvent` by those properties either, so
  both pairs were removed (or never added) as unused DDL weight. Add an index
  in the same change as the query that needs it. Keep package ownership and
  repository publication out of schema assumptions unless reducer admission
  owns that truth.
- **NornicDB composite constraint parity** — `nornicDBSchemaConstraint` drops
  composite `IS UNIQUE` constraints because NornicDB rejects that syntax. The
  NornicDB dialect uses `uid` uniqueness constraints and lookup indexes for the
  same labels; projector code must derive canonical `uid` values from the same
  identity tuple before graph write instead of trusting caller-supplied IDs.
- **No import cycles** — `CypherStatement` and `CypherExecutor` are defined
  here, not imported from `storage/cypher`. Do not add an import of
  `internal/storage/cypher` or any package that imports it.
- **Compatibility is explicit and latest-marker based** —
  `schema_application.go` owns graph schema fingerprints and compatible writer
  fingerprints. Destructive schema changes must leave compatibility empty so
  stale graph writers fail before writing.

## Common changes and how to scope them

- **Add a new node label to the schema** → add a constraint entry to
  `schemaConstraints` in `schema.go` and, if the label needs uid-uniqueness
  with NornicDB, add it to `uidConstraintLabels`. Run
  `go test ./internal/graph -count=1` and `go test ./internal/storage/cypher -count=1`.
  Update the active ADR chunk status row.
  A label any writer MERGEs on `{uid:}` needs a uid constraint
  (`uidConstraintLabels`) or a uid index in the Neo4j DDL (the Neo4j-only
  `neo4jUIDLookupIndexes` list keeps the NornicDB fingerprint unchanged): the
  Neo4j entity-id anchor (`codemodel.Neo4jEntityIDAnchor`) can only seek
  those, and `TestNeo4jEntityIDAnchorCoversEveryUIDWriter` fails otherwise
  (#7057). Then copy the label into the matching anchor list in
  `query/codemodel`.

- **Add a new entity merge path** → if it is a single merge, use
  `BuildEntityMergeStatement` or `MergeEntity`. If it is bulk, add a
  `BatchEntityRow` slice and call `BatchMergeEntities`. Write a test in
  `entity_test.go` or `batch_test.go` first.

- **Add a new backend dialect** → add a `SchemaBackend` constant, extend
  `schemaDialectForBackend` in `schema.go`, and add tests in `schema_test.go`.
  Keep dialect logic inside `schema.go`; do not branch on backend in
  `entity.go` or `batch.go`.

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

## Anti-patterns specific to this package

- **Importing `internal/storage/cypher` from here** — creates a cycle.
  `CypherStatement` and `CypherExecutor` are intentionally duplicated.

- **Backend-conditional logic in `entity.go` or `batch.go`**
  — dialect differences belong only in `schema.go`'s dialect helpers and in
  `internal/storage/cypher` adapters.

- **Skipping `ValidateCypherLabel` on dynamic input** — unsanitized labels or
  property keys produce invalid Cypher that the backend rejects at runtime,
  usually with an opaque parse error.
