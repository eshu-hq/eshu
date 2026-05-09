# AGENTS.md - internal/parser/groovy guidance

## Read first

1. README.md - package boundary, metadata fields, and invariants
2. doc.go - godoc contract for the Groovy helper package
3. metadata.go - Jenkins/Groovy regex extraction and payload compatibility map
4. metadata_test.go - behavior coverage for delivery metadata and map shape

## Invariants this package enforces

- Dependency direction stays one way: parent parser code may import this
  package, but this package must not import internal/parser.
- PipelineMetadata returns typed evidence; Metadata.Map is the compatibility
  bridge for existing payload consumers.
- Shared library versions are normalized away before returning metadata.

## Common changes and how to scope them

- Add Jenkins/Groovy evidence by writing a focused test in metadata_test.go
  first.
- Keep file reading and pre-scan wiring in the parent parser package.
- Keep query-specific enrichment out of this package.

## Failure modes and how to debug

- Missing shared libraries usually means the annotation or library step regex
  did not match the Jenkinsfile form.
- Missing Ansible hints usually means the shell command was not extracted
  before ansible-playbook matching.
- Query regressions usually mean Metadata.Map drifted from the legacy payload
  shape.

## Anti-patterns specific to this package

- Importing the parent parser package to reuse payload helpers.
- Returning only map[string]any and losing the typed helper contract.
- Adding repository-specific Jenkins conventions without fixture evidence.

## What NOT to change without an ADR

- Do not move query or relationship interpretation into this package. It owns
  parser evidence only.
