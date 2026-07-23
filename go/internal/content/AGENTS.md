# AGENTS.md — internal/content guidance for LLM assistants

## Read first

1. `go/internal/content/README.md` — ownership boundary, exported surface, and
   invariants
2. `go/internal/content/writer.go` — `Writer`, `Materialization`, `Record`,
   `EntityRecord`, `Result`, `MemoryWriter`, `CanonicalEntityID`
3. `go/internal/content/dependency_identity.go` —
   `CanonicalEntityIDWithMetadata`, `CanonicalDependencyEntityID`,
   `dependencyIdentityPackageManagers`, `dependencyIdentityDiscriminator`
4. `go/internal/content/writer_config.go` — `WriterConfig`, `LoadWriterConfig`,
   `ContentEntityBatchSizeEnv`, `MaxContentEntityBatchSize`
5. `go/internal/content/doc.go` — package contract for godoc consumers

## Invariants this package enforces

- **ID stability** — `CanonicalEntityID` lower-cases `entityType` and trims all
  inputs before hashing with BLAKE2s. Any caller that normalizes differently
  produces divergent IDs. The prefix `"content-entity:e_"` plus 12 hex chars is
  a hard contract; changing it breaks content-store queries across upgrades.
- **Dependency identity gate is narrow, not `config_kind` alone** —
  `CanonicalEntityIDWithMetadata` only routes to the section-keyed
  `CanonicalDependencyEntityID` when entityType is `"variable"` AND
  `metadata["config_kind"] == "dependency"` AND
  `metadata["package_manager"] ∈ dependencyIdentityPackageManagers` (`npm`,
  `composer`, `cargo`, `gradle`, `maven`, `nuget`, `pypi`, `go`, `rubygems`,
  `pub`, `hex` as of #5507; NOT `swift` — its only producer is a lockfile) AND
  `metadata["lockfile"]` is not true AND `metadata["section"]` is a non-empty
  string. `config_kind == "dependency"` alone is also set by lockfile parsers,
  which legitimately repeat a package name per section (nested transitive
  versions); widening this gate collapses those into one identity. Do not add
  a `package_manager` to the allow-list without proving that format's parser
  guarantees per-section name uniqueness — directly, or through a proven
  `dependencyIdentityDiscriminator` case (see the next bullet).
- **Per-manager discriminators for formats where `(section, name)` alone is
  not enough** — `dependencyIdentityDiscriminator` in
  `dependency_identity.go` folds a package-manager-specific extra component
  into the hashed name for cargo (manifest TOML-key alias, defending against
  `{ package = "..." }` re-aliasing the same crate twice in one section),
  gradle (resolved version, defending against the same coordinate declared
  twice under one configuration), maven (classifier + type, defending against
  co-installed classifier variants like native-build artifacts), nuget
  (item-level MSBuild `Condition`, falling back to group-level — an OVERRIDE,
  not an AND; defends the common `.csproj` multi-targeting pattern where
  `Condition` is set once per ItemGroup, with a narrow accepted residual gap
  when an item-level `Condition` string happens to repeat across two
  different-TFM ItemGroups — see that case's doc comment), pypi (sorted
  extras + marker + value, defending against `requests[socks]` vs
  `requests[toml]`, platform-gated markers, and — as of PR #5731 review —
  `requests>=2` vs `requests<3` repeated-line version constraints; pip's own
  parser does not reject or de-duplicate these at parse time, the same
  toolchain-permits-duplicates shape as gomod below), and go/gomod (the raw
  declared version, defending against a hand-edited or merge-conflicted,
  not-yet-`go mod tidy`-run go.mod whose non-deduplicating `modfile.Parse`
  admits the same module required twice at different versions in one
  section). Only npm, composer, rubygems,
  pub, and hex return an empty discriminator, because each one's parser
  already guarantees per-section name uniqueness on its own (a JSON/YAML map
  key, or the ecosystem's own tooling rejecting a duplicate declaration). Do
  not add or change a case without documenting, in that function's doc
  comment, the concrete manifest feature that requires it — and do not assume
  a format is safe without a discriminator just because its happy-path parser
  output looks unique; check what the underlying format/toolchain actually
  permits (gomod's `go` case is the cautionary example: modfile.Parse does
  NOT de-duplicate, unlike `go mod tidy`'s output).
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
  completeness invariant reaping depends on. This is a ONE-TIME identity
  migration each time it happens (#5329's `line_number` fix, #5357's
  npm/composer section-keying, #5507's remaining nine formats): no schema or
  generation-epoch bump is required, because the reap's per-path anti-join
  (`entity_id <> ALL(freshIDs)`) already evicts any id that is not part of the
  current Write() call's fresh set, regardless of why the id changed. Do not
  invent a parallel migration mechanism; add a regression test alongside the
  existing ones in `content_writer_reap_test.go` (or a same-package sibling
  file) proving the specific old-id/new-id pair for the format you touched.

- **Widen the `CanonicalEntityIDWithMetadata` dependency gate or add a
  `dependencyIdentityDiscriminator` case** → do not add a `package_manager`
  value, relax the `lockfile`/`section` conditions, or add/change a
  discriminator case without proving the target manifest format's parser
  guarantees per-section uniqueness (directly, or through the new
  discriminator). Update both call sites (`internal/content/shape/
  materialize.go` and `internal/projector/runtime.go`'s
  `buildContentEntityRecord`) together — in practice this requires no code
  change in either, since both already call `CanonicalEntityIDWithMetadata`
  generically, but add a lockstep test proving so (see
  `runtime_dependency_identity_test.go`). Add scoping-guard and
  distinctness/reorder-stability test cases (see `dependency_identity_*_test.go`
  for the #5507 pattern). Regenerate the golden corpus cassettes/snapshot only
  if a fixture's asserted content-entity id actually changes — check with
  `rg 'content-entity:e_' testdata/golden/e2e-20repo-snapshot.json
  testdata/cassettes/` first; #5507 touched none (the only hardcoded ids in
  the snapshot are for an unrelated `find_code`/`resolve_entity` "main"
  function lookup).

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

- Symptom: an in-scope manifest dependency's entity ID changes on every sync
  even though the dependency did not move → likely cause: the metadata map
  reaching `CanonicalEntityIDWithMetadata` is missing `section`,
  `config_kind`, or `package_manager`, so the row falls through to the
  legacy line-keyed `CanonicalEntityID` instead of the section-keyed
  `CanonicalDependencyEntityID`. Check `entityMetadataFromPayload` (projector)
  or the cloned `indexed.item.Metadata` (shape) actually carries those keys.
  For a discriminated format (cargo/gradle/maven/nuget/pypi/go), also check
  the discriminator source field itself (`manifest_name`, `value`,
  `dependency_classifier`/`dependency_type`, `condition`, `extras`/`marker`)
  is present and stable across the reorder — a discriminator field that is
  itself line-derived would silently reintroduce the churn this migration
  removes.

- Symptom: a duplicate manifest declaration silently disappears (fewer
  content_entities rows than source lines, no error/telemetry) → likely
  cause: the format's own parser/toolchain does NOT reject or de-duplicate a
  same-section, same-name redeclaration the way `dependencyIdentityPackageManagers`
  assumed, so two genuinely different rows hash to the same
  `CanonicalDependencyEntityID` and `content_writer.go`'s dedupe keeps only
  one. This was a real regression caught in #5507 review for `go` (gomod):
  `golang.org/x/mod`'s `modfile.Parse` does not de-duplicate `require`
  directives, so an untidied go.mod can legitimately require the same module
  twice at different versions. Before assuming a format needs no
  discriminator, verify what the underlying file format/toolchain actually
  permits, not just what a `go mod tidy`-clean or otherwise well-formed
  example looks like. A second instance of the same symptom was caught in
  #5507 PR #5731 review for `pypi`: a first pass reasoned that pip's resolver
  merges same-name constraint lines (`requests>=2` / `requests<3`) into one
  intersected specifier, so omitting `value` from the discriminator was
  "intentional." That conflated pip's install-time resolution behavior with
  what pip's own requirements-file *parser* permits — empirically verified
  (pip 26.1.2) to NOT reject or de-duplicate two same-name lines with
  different constraints at parse time, the identical toolchain-permits-
  duplicates shape as gomod above. `value` is now folded into the pypi
  discriminator (`dependencyExtrasMarkerAndValue`) for the same reason.

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
- `writer_dependency_identity_test.go` — the #5357 scoping-guard table
  (`CanonicalEntityIDWithMetadata`'s five-condition gate) and domain-separation
  proof
- `dependency_identity_cargo_test.go`, `dependency_identity_gradle_test.go`,
  `dependency_identity_maven_test.go`, `dependency_identity_nuget_test.go`,
  `dependency_identity_pypi_test.go`, `dependency_identity_no_discriminator_test.go`
  — the #5507 per-format admits-in-scope, reorder-no-churn, and
  distinctness/discriminator proofs
- `postgres_writer_test.go` — Postgres adapter integration (requires Postgres)

Most test files are `package content` (internal) so they can exercise
unexported helpers (`dependencyIdentitySection`, `dependencyIdentityDiscriminator`,
`metadataStringValue`, ...). Only `postgres_writer_test.go` is
`package content_test` (external), for its Postgres adapter integration
surface. Match whichever a new test file needs, and do not convert an existing
file's package without re-checking what it currently exercises.
