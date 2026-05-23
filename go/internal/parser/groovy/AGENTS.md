# AGENTS.md - internal/parser/groovy

## Read First

1. `README.md` and `doc.go`.
2. `metadata.go`.
3. `metadata_test.go` and parent Groovy parser tests when wrapper behavior
   changes.

## Guardrails

- MUST NOT import `internal/parser`; parent wrappers own file reading, registry
  dispatch, compatibility payload assembly, and pre-scan wiring.
- MUST keep `PipelineMetadata` typed and `Metadata.Map` compatible with legacy
  payload consumers.
- MUST normalize shared library versions before returning metadata.
- MUST keep Jenkinsfile and `vars/*.groovy` roots as metadata evidence only; do
  not resolve dynamic Groovy dispatch, closure delegates, or Jenkins shared
  library loading here.
- MUST NOT move query or relationship interpretation into this package.

## Change Scope

- Add Jenkins/Groovy behavior with a failing `metadata_test.go` or parent parser
  test first.
- Keep query-specific enrichment and repository-specific conventions out unless
  fixture evidence and downstream contract changes are included.
