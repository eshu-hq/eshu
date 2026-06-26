# AGENTS.md — pkg/log guidance for LLM assistants

## Read first

1. `go/internal/telemetry/contract.go` — frozen log key constants this package references.
2. `go/internal/telemetry/README.md` — full telemetry inventory.
3. `docs/internal/design/observability-standards.md` — cross-cutting conventions.

## Invariants

- **Telemetry-backed keys must reference `telemetry.LogKey*` constants**, never
  duplicate the string literal.  A contract rename must propagate automatically.
- **High-cardinality keys (RepoPath, IntentID, WorkerID, RepositoryID,
  WorkloadID) must never appear in OTEL metric labels.**  These are log-only.
- **The `With*` context slot is forward-compatibility only.**  Do not remove or
  rename the `ctx` parameter without updating every call site across the
  codebase.
- **Key constant names must match the wire value** (e.g. `KeyTenantID = "tenant_id"`).
  The test `TestAttrKeysAreStable` enforces this.

## Common changes

### Adding a new key that overlaps with the telemetry contract

Reference `telemetry.LogKey*` directly in the function body.  Add the function
to the "telemetry-backed constructors" section of `README.md`.

### Adding a new key that does not exist in the telemetry contract

1. Define a `Key*` constant in `log.go` with the canonical wire value.
2. Add a constructor function immediately below the constant block.
3. Add the key to `TestAttrKeysAreStable` and `TestPkgLogOwnedKeys`.
4. Update the "Package-owned constructors" table in `README.md`.
5. If the key carries high-cardinality data (paths, IDs, names), note it in a
   doc comment and in the README gotchas section.

### Adding a new `With*` helper

1. Add it in the "context-aware With* helpers" section of `log.go`.
2. Add a test case to `TestWithHelpersMatchAttrConstructors`.
3. Update both the `README.md` table and the observability-standards doc.

## Failure modes

- **Duplicate key constants across packages.**  If two packages define `const
  KeyFoo = "foo"` independently, a rename in one leaves the other stale.
  Always check `telemetry/contract.go` before adding a key here.
- **Accidental metric use of a high-cardinality log key.**  If `RepoPath` or
  `RepositoryID` appears in a metric attribute, Prometheus label cardinality
  explodes.  The cardinality audit test gate (issue #3818) catches this at CI time.

## What not to change without discussion

- The `ctx` parameter on `With*` functions — call sites depend on the
  signature for consistency.
- Any key constant wire value — this breaks log correlation across binaries.
