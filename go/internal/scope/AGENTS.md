# AGENTS.md — internal/scope guidance for LLM assistants

## Read first

1. `go/internal/scope/README.md` — lifecycle diagram, exported surface, and
   invariants
2. `go/internal/scope/scope.go` — `IngestionScope`, `ScopeGeneration`,
   `GenerationStatus`, and all enum constants
3. `go/internal/scope/doc.go` — package contract statement
4. `go/internal/storage/postgres/` — Postgres rows that use these types as
   their in-memory shape

## Invariants this package enforces

- **Transition table is authoritative** — `allowedGenerationTransitions`
  defines every allowed status move. `TransitionTo` enforces it; forbidden
  moves return an error. Do not bypass it by setting `Status` directly.
- **Terminal states do not move** — `GenerationStatusSuperseded`,
  `GenerationStatusCompleted`, and `GenerationStatusFailed` have no outgoing
  transitions. `IsTerminal` reports this.
- **`PreviousGenerationExists` is the prior-generation gate** — do not use
  `ActiveGenerationID != ""` as a proxy. A scope whose first generation failed
  has no active generation ID but does have a prior generation row.
  `HasPriorGeneration` returns `PreviousGenerationExists` directly.
- **`IngestedAt` must not precede `ObservedAt`** — `ScopeGeneration.Validate`
  enforces this; constructors must set both timestamps correctly.
- **Blank identifiers are rejected** — `Validate` rejects blank `ScopeID`,
  `SourceSystem`, `ScopeKind`, `CollectorKind`, and `PartitionKey`. Every scope
  must provide all five.

## Common changes and how to scope them

- **Add a new `ScopeKind`** → add the constant to `scope.go`; `Validate` uses
  `validateIdentifier` (string non-blank check) and does not enumerate known
  kinds, so no switch update is needed; add a test in `scope_test.go` confirming
  the new kind passes validation.

- **Add a new `CollectorKind`** → same pattern as `ScopeKind`; confirm any
  downstream switch on `CollectorKind` in `internal/collector` or
  `internal/storage/postgres` covers the new value.

- **Add a new `GenerationStatus`** → add the constant; update the `switch` in
  `GenerationStatus.Validate`; update `allowedGenerationTransitions`; update
  `GenerationStatus.IsTerminal` if it is terminal; update the diagram in
  `README.md`; add tests covering valid transitions into and out of the new
  state.

- **Add a new field to `IngestionScope` or `ScopeGeneration`** → the field must
  be additive; update `Validate` if the field has constraints; update
  `MetadataCopy` pattern if the field is a map; add Postgres column in
  `internal/storage/postgres` in the same PR.

## Failure modes and how to debug

- Symptom: `Validate` returns "scope_id must not be blank" or similar →
  the collector or ingest path is constructing an `IngestionScope` without
  all required fields; check the ingest code that creates `IngestionScope`
  from the collector payload.

- Symptom: `TransitionTo` returns "cannot transition generation status from X
  to Y" → a caller is attempting a forbidden status move; check the transition
  table diagram in `README.md`; the most common forbidden move is from a
  terminal state or from `pending` directly to `completed`.

- Symptom: projector skips cleanup on a re-ingested scope → `HasPriorGeneration`
  returned false because `PreviousGenerationExists` was not set by the ingest
  path; check the Postgres query in `internal/storage/postgres` that populates
  `PreviousGenerationExists` when constructing `IngestionScope`.

- Symptom: `ValidateForScope` returns scope_id mismatch → the `ScopeGeneration`
  was constructed with a different `ScopeID` than the `IngestionScope` it is
  being paired with; check the ingest path that pairs generations with scopes.

## Anti-patterns specific to this package

- **Setting `Status` directly on `ScopeGeneration`** — always use `TransitionTo`
  or the named helpers (`MarkActive`, `MarkCompleted`, `MarkSuperseded`,
  `MarkFailed`). Direct field assignment bypasses the transition table and
  creates invalid state.

- **Using `ActiveGenerationID != ""` as a prior-generation check** — use
  `HasPriorGeneration` instead. `ActiveGenerationID` is not set for scopes
  whose most recent generation failed before being promoted to active.

- **Adding I/O or package-level state** — this is a pure value package.
  No database connections or global variables belong here.

## What NOT to change without an ADR

- `GenerationStatus` string values — these are stored on disk and appear in
  the Postgres `generation_status` column; changing a value string without a
  migration corrupts existing rows.
- `allowedGenerationTransitions` contents — removing a valid transition breaks
  existing workflows; adding a transition that bypasses terminal states breaks
  the lifecycle invariant relied on by the projector and reducer.
- `ScopeKind` and `CollectorKind` string values — stored on disk in scope rows.
