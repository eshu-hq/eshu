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

## Verification

Run the contract and parent reducer tests, package-doc verification, and
whole-module build and vet. Family extraction work must also compile one moved
family in scratch before the move lands.
