# AGENTS.md - internal/parser/shared

## Read First

1. `README.md` and `doc.go`.
2. `shared.go`.
3. `shared_test.go` and parent parser docs before changing shared contracts.

## Guardrails

- MUST NOT import `internal/parser`, collector, query, projector, reducer,
  storage, or telemetry packages.
- MUST stay language-neutral. A helper used by one adapter belongs in that
  adapter package.
- MUST preserve `BasePayload`, bucket names, default bucket types, option
  normalization, and deterministic `line_number` then `name` sorting.
- MUST keep tree-sitter helpers thin and source-order preserving; do not hide
  language semantics in shared traversal helpers.
- MUST preserve Go semantic-root option meaning, including empty method lists
  for imported interface escapes and lower-case qualified direct-method roots.

## Change Scope

- Add a shared helper only after at least two language packages need it.
- Add a failing `shared_test.go` case before changing payload shape, ordering,
  option normalization, or traversal behavior.
- Do not move registry dispatch, runtime caching, or language-specific parser
  behavior into this package without architecture-owner approval.
