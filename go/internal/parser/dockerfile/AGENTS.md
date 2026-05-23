# AGENTS.md - internal/parser/dockerfile guidance

## Read first

1. README.md - package boundary, metadata fields, and invariants
2. doc.go - godoc contract for the Dockerfile helper package
3. metadata.go - Dockerfile instruction parsing and payload compatibility map
4. tokens.go - Dockerfile escape directive detection and command-line token
   splitting for quoted and escaped metadata values
5. metadata_test.go - behavior coverage for runtime metadata and map shape

## Invariants this package enforces

- Dependency direction stays one way: parent parser code may import this
  package, but this package must not import internal/parser.
- RuntimeMetadata returns typed evidence; Metadata.Map is the compatibility
  bridge for existing payload consumers.
- Row ordering must stay deterministic because parser payloads are fact inputs.

## Common changes and how to scope them

- Add Dockerfile evidence by writing a focused test in metadata_test.go first.
- Keep file reading and registry dispatch in the parent parser package.
- Keep query-specific runtime story generation out of this package.

## Failure modes and how to debug

- Missing runtime rows usually mean instruction continuation or key/value token
  parsing changed.
- Query regressions usually mean Metadata.Map drifted from the legacy payload
  shape expected by parser.ExtractDockerfileRuntimeMetadata consumers.

## Anti-patterns specific to this package

- Importing the parent parser package to reuse payload helpers.
- Returning only map[string]any and losing the typed helper contract.
- Adding repository-specific Dockerfile conventions without fixture evidence.

## What NOT to change without an ADR

- Do not move query or relationship interpretation into this package. It owns
  parser evidence only.
