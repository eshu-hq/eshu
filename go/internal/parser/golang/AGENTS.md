# AGENTS.md - internal/parser/golang guidance

## Read first

1. README.md - package boundary, embedded SQL behavior, and invariants
2. doc.go - godoc contract for the Go helper package
3. embedded_sql.go - SQL literal extraction and line-number accounting
4. embedded_sql_test.go - behavior coverage for SQL APIs and escaped literals

## Invariants this package enforces

- Dependency direction stays one way: parent parser code may import this
  package, but this package must not import internal/parser.
- EmbeddedSQLQueries only emits bounded evidence for recognized database/sql
  and sqlx call sites.
- Line numbers must refer to the SQL table reference in the original Go source,
  not in the extracted function body.

## Common changes and how to scope them

- Add SQL API support by writing a focused test in embedded_sql_test.go first.
- Keep map[string]any payload assembly in the parent parser package.
- Keep tree-sitter Go parsing out of this package until the shared node helper
  boundary has a separate design.

## Failure modes and how to debug

- Missing embedded SQL rows usually mean the call-site regex did not recognize
  the database method or the SQL table regex did not match the literal body.
- Wrong line numbers usually mean literal offsets were changed without
  preserving the original source offset.

## Anti-patterns specific to this package

- Importing the parent parser package to reuse payload helpers.
- Treating string literals near any function call as SQL evidence.
- Returning payload maps from this package.

## What NOT to change without an ADR

- Do not move the full Go tree-sitter adapter here until shared tree helpers,
  payload helpers, and registry wiring have explicit package contracts.
