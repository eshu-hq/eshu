# AGENTS.md — fixtures guidance for LLM assistants

## Scope

Pure-Go drift-evidence test helpers only: JSON fixture builders, fake row
padding. No SQL, no DDL, no queue/lease/claim code, no graph writes, no
production code.

## Rules

- Keep every helper behavior-stable: root and tfstate tests depend on exact
  shapes (JSON keys, padding widths). Change behavior only with the issue
  that requires it, and update `fixtures_test.go` first (TDD: failing test
  first).
- Do not add production helpers here; those belong in the sibling
  `filters` package. This package exists only because the root→tfstate
  import direction forbids sharing through either endpoint (issue #8552).
