# Reducer contract package instructions

## Read first

- Repository-root `AGENTS.md`
- `go/internal/reducer/AGENTS.md`
- `go/internal/reducer/contract/README.md`
- `docs/internal/design/package-restructure.md`

## Invariants

- This package must remain a leaf below `internal/reducer`; never import the
  parent reducer package.
- Keep the `Domain` validation set and parent compatibility constants in
  lockstep.
- Preserve `Intent`, `Result`, handler, validation, and retry behavior byte for
  byte when moving code across the boundary.
- Do not add queue, storage, graph, telemetry, or runtime dependencies here.

## Common changes

Add a domain constant and its known-set entry together. Update the 66-domain
characterization test when a new production-registrable domain lands. Changes to
intent or result fields require compatibility tests through the parent aliases.

## Failure modes

- Adding a domain constant without its known-set entry makes parsing reject a
  value that callers can compile.
- Treating a reserved validation identifier as registrable makes the registry
  accept a domain with no production owner.
- Mutating a nested payload value after `Intent.Clone` also changes the source
  intent because only the top-level map is detached.

## Anti-patterns

- Do not import the parent reducer package or move registry composition,
  adapters, queue execution, retries, or telemetry into this leaf.
- Do not treat `KnownDomains` as the production-registrable set without
  excluding the three reserved identifiers.
- Do not describe `Intent.Clone` as a recursive or deep copy.

## Architecture decisions

- #6100 establishes this package as the dependency-neutral contract consumed by
  forthcoming reducer family packages.
- The parent reducer remains the composition root and compatibility surface;
  family extraction must not widen this leaf with runtime-owned dependencies.

## Verification

Run the contract and parent reducer tests, package-doc verification, and
whole-module build and vet. Family extraction work must also compile one moved
family in scratch before the move lands.
