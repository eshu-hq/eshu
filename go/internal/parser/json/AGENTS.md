# AGENTS.md - internal/parser/json

## Read First

1. `README.md` and `doc.go`.
2. `language.go`, `ordered_object.go`, `dbt_manifest.go`,
   `data_intelligence.go`, and `governance.go`.
3. Parent wrapper `../json_language.go` and JSON/dbt parser tests.

## Guardrails

- MUST NOT import `internal/parser`; parent-owned helpers enter through
  `Config`.
- MUST preserve JSON payload bucket names, row fields, document order where the
  ordered decoder has it, and deterministic fallback ordering where maps lose
  order.
- MUST keep JSONC normalization bounded and strict after comments/trailing
  commas are stripped.
- MUST delegate CloudFormation/SAM extraction to `internal/parser/cloudformation`.
- MUST keep dbt SQL lineage parsing parent-owned through `LineageExtractor`.
- MUST NOT read repository state beyond the single file passed to `Parse`.

## Change Scope

- Add JSON/JSONC document support only when selected by bounded filename or
  decoded-shape evidence.
- Add dbt manifest fields in `dbt_manifest.go` with tests proving payload rows
  and coverage state.
- Add replay fixture rows in the domain file that owns that fixture family.
- Do not move parent-exported lineage types, graph/query behavior, or storage
  dependencies into this package.
