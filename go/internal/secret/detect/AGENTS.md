# AGENTS.md — secret/detect guidance for LLM assistants

## Read first

1. `README.md` — the contract and the gaps owned by #8833
2. `go/internal/storage/postgres/migrations/235_content_file_secret_lines_marker_policy.sql` —
   the function this package mirrors (138 holds the table and triggers)
3. `go/internal/query/hardcoded_secret_migration_binding_test.go` — the binding
   test that pins this package to the migration

## Invariants

- Never edit a value here without a new migration that replaces
  `eshu_secret_line_findings` and re-derives the table. Migrations 138 and 235
  are never edited after they ship.
- Keep the marker arm first and keep `REDACTED_value` out of it; the
  content-store redactors depend on both.
- `SQLPattern` stays a concatenation of the three parts. Do not paste a second
  copy of the pattern anywhere; bind to the constant.
- Keep `ArmSources` in migration `CASE` order. The order is behavior.
- No imports from `go/internal/...`; the query layer and the collector both
  import this package.
