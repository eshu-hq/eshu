# AGENTS.md — secret/detect guidance for LLM assistants

## Read first

1. `README.md` — the contract and the gaps owned by #8833
2. `go/internal/storage/postgres/migrations/138_content_file_secret_lines.sql` —
   the SQL this package mirrors
3. `go/internal/query/hardcoded_secret_migration_binding_test.go` — the binding
   test that pins this package to the migration

## Invariants

- Never edit a value here without a new migration that replaces
  `eshu_secret_line_findings` and re-derives the table. Migration 138 is never
  edited after it ships.
- `SQLPattern` stays a concatenation of the three parts. Do not paste a second
  copy of the pattern anywhere; bind to the constant.
- Keep `ArmSources` in migration `CASE` order. The order is behavior.
- No imports from `go/internal/...`; the query layer and the collector both
  import this package.
