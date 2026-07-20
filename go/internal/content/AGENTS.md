# AGENTS.md — internal/content guidance for LLM assistants

## Read first

1. `go/internal/content/README.md` — ownership boundary, exported surface, and
   invariants
2. `go/internal/content/writer.go` — `Writer`, `Materialization`, `Record`,
   `EntityRecord`, `Result`, `MemoryWriter`, `CanonicalEntityID`,
   `CanonicalEntityIDWithMetadata`, `CanonicalDependencyEntityID`
3. `go/internal/content/writer_config.go` — `WriterConfig`, `LoadWriterConfig`,
   `ContentEntityBatchSizeEnv`, `MaxContentEntityBatchSize`
4. `go/internal/content/doc.go` — package contract for godoc consumers

## Invariants this package enforces

- **ID stability** — `CanonicalEntityID` lower-cases `entityType` and trims all
  inputs before hashing with BLAKE2s. Any caller that normalizes differently
  produces divergent IDs. The prefix `"content-entity:e_"` plus 12 hex chars is
  a hard contract; changing it breaks content-store queries across upgrades.
- **Dependency identity gate is narrow, not `config_kind` alone** —
  `CanonicalEntityIDWithMetadata` only routes to the section-keyed
  `CanonicalDependencyEntityID` when entityType is `"variable"` AND
  `metadata["config_kind"] == "dependency"` AND
  `metadata["package_manager"] ∈ {"npm", "composer"}` AND
  `metadata["lockfile"]` is not true AND `metadata["section"]` is a non-empty
  string. `config_kind == "dependency"` alone is also set by lockfile parsers,
  which legitimately repeat a package name per section (nested transitive
  versions); widening this gate collapses those into one identity. Do not add
  a `package_manager` to the allow-list without proving that format's parser
  guarantees per-section name uniqueness.
- **Two-site lockstep** — `internal/content/shape.Materialize` and
  `internal/projector`'s `buildContentEntityRecord` `entity_id` fallback both
  call `CanonicalEntityIDWithMetadata` with the same metadata view. The
  projector fallback only fires for facts without a collector-minted
  `entity_id` (version skew, replayed cassettes, non-git producers) —
  precisely the path where divergent minting would silently corrupt identity.
- **Clone before retain** — `Record.Clone`, `EntityRecord.Clone`, and
  `Materialization.Clone` exist so callers can safely retain values across async
  boundaries. `MemoryWriter.Write` always clones; concrete Postgres writers must
  do the same before any deferred write.
- **Batch-size guard** — `LoadWriterConfig` rejects values above
  `MaxContentEntityBatchSize` (4000) because exceeding it pushes Postgres past
  its bind-parameter limit. Do not raise the cap without confirming the Postgres
  adapter stays within `pgx` limits.
- **No Postgres dependency** — this package must not import `database/sql`,
  `pgx`, or any `internal/storage` sub-package. The writer interface boundary
  is how the projector stays decoupled from the storage adapter.

## Common changes and how to scope them

- **Add a field to `EntityRecord`** → add the field in `writer.go`, update
  `EntityRecord.Clone` if the new field contains a reference type, update the
  Postgres writer in `internal/storage/postgres`, add or update the test in
  `writer_test.go`. Do not add the field here without updating the storage
  adapter — partial fields produce silent NULL columns.

- **Change `CanonicalEntityID` or `CanonicalDependencyEntityID` inputs** →
  this is a breaking change. Every existing `content_entities` row carries a
  stored ID; changing the hash inputs means old and new IDs diverge. Discuss
  schema migration before touching either function. A migration relies on the
  Postgres reap in `internal/storage/postgres/content_writer_reap.go` to evict
  stale ids on next re-ingest — see that file's doc comment for the
  completeness invariant reaping depends on.

- **Widen the `CanonicalEntityIDWithMetadata` dependency gate** → do not add a
  `package_manager` value or relax the `lockfile`/`section` conditions without
  proving the target manifest format's parser guarantees per-section name
  uniqueness (the same proof `package.json` and `composer.json` have). Update
  both call sites (`internal/content/shape/materialize.go` and
  `internal/projector/runtime.go`'s `buildContentEntityRecord`) together, add
  scoping-guard test cases in `writer_test.go`, and regenerate the golden
  corpus cassettes/snapshot since minted content-entity ids change.

- **Tune the batch size** → set `ESHU_CONTENT_ENTITY_BATCH_SIZE` at runtime.
  Raising `MaxContentEntityBatchSize` (4000) requires confirming the Postgres
  adapter does not exceed the `pgx` parameter limit for the widest upsert
  statement.

- **Add a `WriterConfig` field** → add the field to `WriterConfig` in
  `writer_config.go`, add an env var constant, add parsing logic in
  `LoadWriterConfig`, add a test in `writer_config_test.go`. Keep
  `LoadWriterConfig` returning an error for any invalid value — callers must
  not silently ignore misconfiguration.

## Failure modes and how to debug

- Symptom: entity IDs diverge between ingester and reducer projections →
  likely cause: caller passes entity type in a different case or with extra
  whitespace before `CanonicalEntityID`. Check normalizations in
  `internal/content/shape/materialize.go`.

- Symptom: an npm/composer manifest dependency's entity ID changes on every
  sync even though the dependency did not move → likely cause: the metadata
  map reaching `CanonicalEntityIDWithMetadata` is missing `section`,
  `config_kind`, or `package_manager`, so the row falls through to the
  legacy line-keyed `CanonicalEntityID` instead of the section-keyed
  `CanonicalDependencyEntityID`. Check `entityMetadataFromPayload` (projector)
  or the cloned `indexed.item.Metadata` (shape) actually carries those keys.

- Symptom: two distinct nested lockfile dependency versions (e.g. `react@17`
  and `react@18` in `package-lock.json`) collapse into one content-entity row
  → likely cause: someone widened the dependency gate to match
  `config_kind == "dependency"` without also requiring `lockfile` not true.
  Lockfile parsers set `metadata["lockfile"] = true` specifically so this
  gate excludes them; see the Gotchas note on why.

- Symptom: Postgres upsert exceeds parameter limit → likely cause:
  `ESHU_CONTENT_ENTITY_BATCH_SIZE` set above 4000, or the Postgres adapter
  computes batch width incorrectly. `LoadWriterConfig` will reject values
  above `MaxContentEntityBatchSize` at startup; check that config loading is
  being called.

- Symptom: `MemoryWriter` returns wrong `DeletedCount` in tests → `DeletedCount`
  counts both `Record.Deleted` and `EntityRecord.Deleted`. Make sure both slices
  are populated in the test fixture.

## Testing

Gate: `cd go && go test ./internal/content -count=1`

Key test files:

- `writer_test.go` — `Materialization`, `Clone`, `MemoryWriter`, `CanonicalEntityID`
- `writer_config_test.go` — `LoadWriterConfig` valid and invalid inputs
- `postgres_writer_test.go` — Postgres adapter integration (requires Postgres)

All test files are `package content_test` (external). Do not convert them to
`package content` without re-checking export visibility.
