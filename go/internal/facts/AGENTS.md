# AGENTS.md — internal/facts guidance for LLM assistants

## Read first

1. `go/internal/facts/README.md` — purpose, ownership boundary, exported
   surface, and invariants
2. `go/internal/facts/models.go` — `Envelope`, `Ref`, `ScopeGenerationKey`,
   `Clone`
3. `go/internal/facts/stableid.go` — `StableID`, the SHA-256 normalization path
4. `go/internal/facts/doc.go` — package contract statement

## Invariants this package enforces

- **Additive-only fields** — `Envelope` and `Ref` are on-disk contracts. Removing
  or renaming any field breaks stored rows. Every new field must be optional and
  back-compatible.
- **Payload immutability after handoff** — `Envelope.Payload` is a
  `map[string]any`. Once an envelope is handed to a downstream stage, the map
  must not be mutated. Use `Clone` when branching or replaying.
- **StableID determinism** — `StableID` normalizes `time.Time` values to UTC
  RFC3339Nano and sorts map keys via `json.Marshal`. Do not change the
  normalization without migrating all stored stable keys.
- **Tombstone handling** — `IsTombstone` is a first-class field. Any stage that
  writes graph nodes or content rows must check this flag and take the deletion
  path, not the upsert path.

- **Collector provenance** — new fact emitters must set `CollectorKind` and
  `SourceConfidence`. `CollectorKind` names the collector family. Use
  `SourceConfidence` to say whether the claim was `observed`, `reported`,
  `inferred`, or `derived`. Treat `unknown` as a compatibility fallback, not as
  the expected value for new collector work.

## Common changes and how to scope them

- **Add a new field to `Envelope`** → add it with a zero value default;
  ensure `Clone` copies it if it is a reference type (map, slice, pointer);
  update the test in `models_test.go`; confirm the Postgres column is added in
  `internal/storage/postgres` in the same PR.

- **Add a new field to `Ref`** → same additive-only rule; check that all
  callers that construct `Ref` literals compile without modification.

- **Change `StableID` normalization** → first understand whether existing stored
  keys must be migrated. If yes, write a migration before merging. The stable key
  is used as a deduplication signal across ingestion runs; changing it changes
  which facts are considered "same as before."

## Failure modes and how to debug

- Symptom: projector loads facts with missing `FactKind` or blank `ScopeID` →
  likely cause: collector or parser emitted a partial envelope → check the
  ingester structured logs for the ingest step that produced this generation;
  look for `FactKind = ""` or `ScopeID = ""` in the Postgres facts table.

- Symptom: two runs produce different `StableFactKey` for the same source
  record → likely cause: non-deterministic map iteration in the identity
  argument passed to `StableID` → `StableID` normalizes via `json.Marshal`
  which sorts keys; if keys differ between runs, the identity map itself is
  inconsistent.

- Symptom: `Clone` returns a shallow copy that still shares mutable state →
  `Clone` deep-copies maps and slices but does not copy custom types embedded
  inside `any` values. If `Payload` holds a struct pointer, callers must handle
  that themselves.

## Anti-patterns specific to this package

- **Adding caller-specific convenience fields** — `doc.go` states this
  explicitly: convenience fields that only help one caller belong elsewhere.
  Keep `Envelope` and `Ref` minimal.

- **Adding I/O or package-level state** — this is a leaf contract package.
  No database connections, HTTP clients, or global variables belong here.

- **Using `Envelope` as a mutation target** — treat `Envelope` as a value type.
  Use `Clone` before mutating for downstream stages, and never pass a
  non-cloned envelope to two concurrent goroutines.

## What NOT to change without an ADR

- The `Envelope` wire shape — any change that affects Postgres serialization or
  cross-stage interchange requires a migration plan and ADR.
- `StableID` normalization behavior — changing it silently changes fact
  deduplication across ingestion runs.
