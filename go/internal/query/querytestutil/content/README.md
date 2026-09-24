# querytestutil/content

## Purpose

The content half of `querytestutil`, nested under it for #6642. See `doc.go`
for the godoc contract and the parent `../README.md` for why shared test
doubles live in ordinary `.go` files.

## Ownership boundary

Same as the parent: helpers used by more than one package's tests, and no
production behavior. A helper used by one package belongs in that package's own
`_test.go` file.

## Exported surface

See `doc.go`.

## Dependencies

Leaf packages only, as the parent's invariant 2 states. This package does not
import the parent `querytestutil` or its sibling leaf.

## Telemetry

None. A test double deliberately emits no telemetry.

## Gotchas / invariants

A non-test file under `internal/query` importing this package fails
`internal/queryplan`'s callsite inventory, the same as an import of the parent.

## Related docs

`../README.md`, `../AGENTS.md`.
