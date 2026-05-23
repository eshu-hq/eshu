# AGENTS.md - internal/parser/dockerfile

## Read First

1. `README.md` and `doc.go`.
2. `metadata.go` and `tokens.go`.
3. `metadata_test.go` and parent Dockerfile parser tests when wrapper behavior
   changes.

## Guardrails

- MUST NOT import `internal/parser`; parent wrappers own file reading, registry
  dispatch, and compatibility payload assembly.
- MUST keep `RuntimeMetadata` typed and `Metadata.Map` compatible with legacy
  payload consumers.
- MUST preserve deterministic row ordering because Dockerfile metadata becomes
  parser fact input.
- MUST honor Dockerfile token rules for modeled metadata: continuation escape
  directives, quoted values, legacy `ENV key value`, multi-argument `ARG`,
  `FROM --platform`, and registry hosts with ports.
- MUST NOT move query or relationship interpretation into this package.

## Change Scope

- Add Dockerfile behavior with a failing `metadata_test.go` or parent parser
  test first.
- Keep query-specific runtime story generation and repository-specific
  conventions out unless fixture evidence and downstream contract changes are
  included.
