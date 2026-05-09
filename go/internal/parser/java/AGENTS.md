# AGENTS.md - internal/parser/java guidance

## Read first

1. README.md - package boundary, supported metadata files, and invariants
2. doc.go - godoc contract for ClassReference and MetadataClassReferences
3. metadata.go - ServiceLoader and Spring metadata extraction
4. metadata_test.go - behavior coverage for continued Spring factories,
   invalid class names, and duplicate suppression

## Invariants this package enforces

- Dependency direction stays one way: parent parser code may import this
  package, but this package must not import internal/parser.
- MetadataClassReferences only emits class references for supported metadata
  paths. Unsupported paths return no evidence.
- Class names must look like fully qualified Java class names. Dynamic,
  invalid, or duplicate names stay out of the result set.
- Ordering follows file order after comment cleanup and continuation handling.
  Do not introduce map iteration into the emitted order.

## Common changes and how to scope them

- Add a new Java metadata file shape by extending the path classifier in
  metadata.go and adding a fixture in metadata_test.go first.
- Add a new field to ClassReference only when the parent parser has a real
  consumer for it. Keep payload map keys in the parent parser package.
- Keep tree-sitter Java source parsing out of this package until the shared
  node helper boundary has a separate design.

## Failure modes and how to debug

- Missing ServiceLoader roots usually mean the path classifier did not match
  the metadata path. Check metadata.go and add a focused test.
- Extra dead-code roots usually mean invalid names were accepted too broadly.
  Tighten the class-name validation before changing reducer policy.
- Wrong line numbers in Spring factories usually mean continuation handling was
  changed without preserving the first line of the joined value.

## Anti-patterns specific to this package

- Importing the parent parser package to reuse payload helpers.
- Returning map[string]any from this package. Typed rows keep the package seam
  small and testable.
- Treating arbitrary strings in metadata as class references.

## What NOT to change without an ADR

- Do not move the full Java tree-sitter adapter here until shared tree helpers,
  payload helpers, and registry wiring have explicit package contracts.
