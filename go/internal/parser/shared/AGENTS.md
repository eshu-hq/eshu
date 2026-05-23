# AGENTS.md - internal/parser/shared guidance

## Read first

1. `README.md` - package boundary and invariants
2. `shared.go` - exported helper contracts used by child parser packages
3. `shared_test.go` - payload and option compatibility coverage
4. `go/internal/parser/README.md` - parent parser ownership and language layout

## Invariants this package enforces

- Dependency direction stays one way: child parser packages may import this
  package, but this package must not import `internal/parser`.
- Helpers must stay language-neutral. Language-specific behavior belongs in the
  language package that owns it.
- Payload bucket shape and deterministic sorting are fact-input contracts.

## Common changes and how to scope them

- Add a shared helper only after at least two language packages need it.
- Add a focused test in `shared_test.go` before changing payload shape,
  ordering, or option normalization.
- Keep tree-sitter helpers as thin wrappers over node APIs; do not hide
  language semantics here.

## Failure modes and how to debug

- Import cycles usually mean a child package reached back into the parent
  parser package instead of using this package.
- Missing content entities usually mean a bucket name or row shape changed.
- Non-deterministic parser output usually means a caller added map iteration
  without sorting before appending rows.

## Anti-patterns specific to this package

- Adding a helper because one adapter wants shorter local code.
- Importing collector, query, projector, reducer, storage, or the parent parser
  package.
- Moving runtime cache ownership here before there is an ADR-level reason.

## What NOT to change without an ADR

- Do not change `BasePayload` bucket names or default bucket types without a
  fact-materialization plan.
- Do not move registry dispatch or parser runtime caching into this package.
