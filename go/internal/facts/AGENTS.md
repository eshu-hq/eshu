# AGENTS.md — internal/facts guidance for LLM assistants

## Read first

1. `go/internal/facts/README.md` — purpose, ownership boundary, exported
   surface, and invariants
2. `go/internal/facts/models.go` — `Envelope`, `Ref`, `ScopeGenerationKey`,
   `Clone`
3. `go/internal/facts/encode/stable.go` — `StableID`, the SHA-256
   normalization path. The facts root's `stableid.go` is now only a forwarder
   to it (issue #6776).
4. `go/internal/facts/doc.go` — package contract statement, including which
   families still live here and which moved to a nested package

## Invariants this package enforces

- **Additive-only fields** — `Envelope` and `Ref` are on-disk contracts. Removing
  or renaming any field breaks stored rows. Every new field must be optional and
  back-compatible.
- **Payload immutability after handoff** — `Envelope.Payload` is a
  `map[string]any`. Once an envelope is handed to a downstream stage, the map
  must not be mutated. Use `Clone` when branching or replaying.
- **StableID determinism** — `StableID` normalizes `time.Time` values to UTC
  RFC3339Nano and sorts map keys via `json.Marshal`. Do not change the
  normalization without migrating all stored stable keys. The derivation bytes
  are pinned by `encode.TestStableIDPinsDerivationBytes` against expectations
  computed outside Go, so a change to the hashed encoding fails there first.
- **Tombstone handling** — `IsTombstone` is a first-class field. Any stage that
  writes graph nodes or content rows must check this flag and take the deletion
  path, not the upsert path.

- **Collector provenance** — new fact emitters must set `CollectorKind` and
  `SourceConfidence`. `CollectorKind` names the collector family. Use
  `SourceConfidence` to say whether the claim was `observed`, `reported`,
  `inferred`, or `derived`. Treat `unknown` as a compatibility fallback, not as
  the expected value for new collector work.

## Nested families and the compat surface

Issue #6776 moved four family groups out of this directory so it drops back
under the 40-non-test-`.go`-file `dirgate` cap: `cloud/`, `code/`, `docs/`,
and `supply/chain/`, plus the shared `encode/` substrate. Read the touched
package's own `AGENTS.md` before changing a family.

- **The dependency runs root → family, never back.** This package imports the
  nested families to build `schemaVersionFamilies` and to re-export their
  pre-move spellings. A nested family that imports `internal/facts` is an
  import cycle; shared substrate belongs in `internal/facts/encode`.
- **`compat_*.go` files are aliases and thin forwarders only.** No logic, no
  new behavior. A later family move adds a stanza to the matching file and
  does not create a new `compat_*.go`. Delete an entry when its last caller
  has moved, not before.
- **A rename inside a nested family is a repo-wide change.** Callers reach
  these names as `facts.X` through the compat surface, so `go build
  ./internal/facts/...` proves nothing about them; run `cd go && go vet ./...`.
- **Do not name a package `documentation`.** `go/build` excludes every `.go`
  file in a package with that name, so it compiles nowhere. That is why the
  documentation family is `docs/`.
- **A root file must not be named after a sibling subpackage.** `dirgate`'s
  naming rule rejects a root file whose stem equals a sibling package name or
  starts with `<name>_`, which is why the supply-chain compat surface is
  `compat_supply_chain.go` and not `supply_chain.go`.
- **This directory has no `dirgate` grandfather row any more.** It sat at 45
  files and is now 27. Growing it back past 40 is a gate failure with no
  ledger row to bump; split into a nested family instead. Issue #6951 holds
  the groups still at root; issue #6950 holds retiring the compat surface.

## Common changes and how to scope them

- **Add a new field to `Envelope`** → add it with a zero value default;
  ensure `Clone` copies it if it is a reference type (map, slice, pointer);
  update the test in `models_test.go`; confirm the Postgres column is added in
  `internal/storage/postgres` in the same PR.

- **Add a new field to `Ref`** → same additive-only rule; check that all
  callers that construct `Ref` literals compile without modification.

- **Add or change a core fact kind registry entry** → update
  `specs/fact-kind-registry.v1.yaml`, regenerate
  `fact_kind_registry.generated.go` and `FACT_KIND_REGISTRIES.md` with
  `bash scripts/verify-fact-kind-registry.sh`, and keep the family constants
  plus schema-version helpers aligned in the same slice — in the owning
  nested package when the family lives in one. Keep every package in this
  tree leaf-only: no collector imports and no I/O.

- **Change `StableID` normalization** → the function lives in
  `internal/facts/encode`. First understand whether existing stored keys must
  be migrated. If yes, write a migration before merging. The stable key
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
