# AGENTS.md — filters guidance for LLM assistants

## Scope

Pure-Go fact-kind filter cleanup only. No SQL, no DDL, no queue/lease/claim
code, no graph writes.

## Rules

- Keep `CleanFactKinds` behavior-stable: root and tfstate production code
  and tests depend on trim/dedup/first-seen-order. Change behavior only
  with the issue that requires it, and update `clean_test.go` first
  (TDD: failing test first).
- Do not add backend, queue, or schema helpers here. This package exists
  only because the root→tfstate import direction forbids sharing through
  either endpoint (issue #8552).
