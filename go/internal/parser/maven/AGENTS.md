# AGENTS.md - internal/parser/maven guidance

## Read first

1. `README.md` - package purpose, ownership boundary, and invariants.
2. `doc.go` - godoc contract for the Maven manifest parser.
3. `parser.go` - `Parse`, dependency row construction, and property
   resolution.
4. `parser_test.go` - behavior coverage for direct dependencies, scopes,
   property substitution, dependencyManagement import, optional flags,
   malformed XML, and multi-module parent references.
5. `../json/dependency_coverage.go` - matrix entry that this package backs.

## Invariants this package enforces

- Dependency direction stays one way: the parent parser package may import
  this package, but this package must not import `internal/parser`.
- Never execute Maven, fetch artifacts, or read sibling POMs. Multi-module
  parent properties must stay unresolved unless they appear in the same
  file's `<properties>`.
- `dependency_resolution_state` must be one of `resolved`, `partial`, or
  `unresolved`; partial/unresolved entries keep `value` as the literal text
  found in the file so callers can see exactly what was declared.
- Missing `groupId` or `artifactId` rows are skipped. Never emit a row that
  has no Maven coordinate.

## Common changes and how to scope them

- Add a new failing test in `parser_test.go` before changing `parser.go`.
- New POM elements (`exclusions`, BOM imports, etc.) belong in `parser.go`
  only when the behavior is provable from one file.
- Telemetry, registry dispatch, and engine wrappers belong in the parent
  parser package, not here.

## Failure modes and how to debug

- Missing dependency rows usually mean the `<dependency>` element lacked
  `<groupId>` or `<artifactId>`, or the XML element nesting drifted.
- Spurious `unresolved` states usually mean a property reference was
  written with whitespace or scope (e.g. `${project.version}`) that we do
  not resolve; check `resolvePropertyReferences`.
- Test runs without `-count=1` may reuse cached results; rerun with
  `-count=1` when iterating on fixtures.

## Anti-patterns specific to this package

- Inventing versions from parent POMs or external repositories.
- Emitting a row when only a `<groupId>` or only a `<artifactId>` is
  present.
- Resolving `${project.version}`-style implicit Maven coordinates that
  require a built model rather than file-local properties.

## What NOT to change without an ADR

- Cross-file parent POM resolution.
- Network or filesystem lookups beyond the file passed to `Parse`.
- The `dependency_resolution_state` vocabulary; reducer and docs depend on
  the three-value set.
