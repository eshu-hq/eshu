# AGENTS.md - querycontract

## Read first

1. `doc.go` for the public contract.
2. `README.md` for the ownership boundary and proof requirements.
3. Parent `../AGENTS.md` for query-wide invariants.

## Invariants

- Keep this package dependency-neutral. It may use the Go standard library but
  must not import the root `query` package, graph drivers, or Postgres adapters.
- Preserve response JSON field names and envelope negotiation.
- Preserve unknown-capability panic text and profile ceiling semantics.
- Register family capabilities through `RegisterCapabilities`; duplicate
  initialization attempts are contract failures.
- Preserve the selector presence tri-state on `K8sSelectCandidate`.

## Verification

Run focused `querycontract` and root `query` tests, then whole-module build and
vet. Run `scripts/verify-package-docs.sh` whenever this package changes.

## Common changes

- Add a shared wire or port type only when multiple query families need it
  without importing the root package.
- Move a capability registration with its family and preserve the canonical
  YAML order through the root ordering gate.
- Keep root aliases and function wrappers until every external caller has a
  separately reviewed migration path.
- To start the per-route handler span from a family package, seed a package-local
  swappable tracer var from `HandlerTracer` and pass it to
  `StartHandlerSpanWith`, the way package `query` does. Do not reassign the
  exported `HandlerTracer` itself: it is shared by every consumer, so a test that
  swaps it mutates state other packages read, and two packages swapping it under
  `t.Parallel()` race.

## Failure modes

- A copied type instead of an alias breaks source identity across storage and
  handler adapters.
- Dropping selector-presence fields collapses absent and present-empty values.
- Root-only capability registration makes a moved family panic when tested by
  itself.
- Reordering capability initialization can change the canonical inventory.

## Anti-patterns

- Do not add handler orchestration, Cypher, SQL, or family-specific response
  models here.
- Do not expose graph or Postgres implementations through the neutral ports.
- Do not replace root function wrappers with mutable function variables.

## ADR-controlled changes

Changing capability overwrite semantics, removing the root compatibility
layer, or moving route assembly into this package requires an accepted
architecture decision before implementation.
