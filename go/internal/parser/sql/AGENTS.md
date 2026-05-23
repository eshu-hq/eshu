# AGENTS.md - internal/parser/sql

## Read First

1. `README.md` and `doc.go`.
2. `language.go`, `shared.go`, `routines.go`, and `migrations.go`.
3. `language_test.go`, `migrations_test.go`, and parent SQL parser tests.

## Guardrails

- MUST NOT import `internal/parser`; parent wrappers own registry dispatch,
  engine routing, repository path handling, and content metadata inference.
- MUST preserve SQL payload bucket names, entity strings, relationship strings,
  row fields, line numbers, deduplication, and deterministic ordering.
- MUST keep migration-tool detection path-sensitive and deterministic.
- MUST keep constraints out of `sql_columns` while preserving bounded table
  references from constraint clauses.
- MUST treat SQL relationships as conservative evidence. Do not turn comments,
  aliases, expressions, or unbounded routine text into table truth.
- MUST keep dbt compiled-model lineage in its parent-owned compatibility path
  unless a separate design moves that boundary.

## Change Scope

- Add DDL or routine support with a failing SQL parser test first, then edit the
  narrow helper that owns that statement shape.
- Add migration support in `migrations.go` with package-local coverage.
- Do not change payload shape without coordinated content shape, fact, fixture,
  reducer/query, and docs updates.
