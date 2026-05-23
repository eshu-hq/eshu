# AGENTS.md - internal/parser/dbtsql

## Read first

1. `README.md` - package purpose, ownership boundary, and invariants.
2. `doc.go` - godoc contract for lineage callers.
3. `lineage.go` - `ColumnLineage`, `CompiledModelLineage`, and projection flow.
4. `expressions.go` - supported transform and unresolved-expression rules.
5. `identifiers.go` - identifier scanning and SQL keyword exclusions.
6. Parent wrapper `../dbt_sql_lineage.go`.
7. JSON caller `../json/dbt_manifest.go`.

## Invariants this package enforces

- Do not import the parent `internal/parser` package.
- Do not infer lineage for expressions outside the bounded supported set.
- Preserve unresolved reasons when expression truth is partial or unknown.
- Keep extraction deterministic across map iteration, CTE order, and projection
  order.

## Common changes

- New transform support requires a positive lineage test and an unresolved case
  proving unsupported shapes still report a reason.
- New SQL syntax support belongs here only when it can be handled from one
  compiled model string without repository scans.
- JSON manifest payload changes belong in `../json`, not this package.

## Failure modes

- Missing column edges usually means relation binding failed in `lineage.go`.
- Overconfident edges usually mean `expressions.go` accepted a dynamic
  expression without enough proof.
- Flaky tests usually mean an added map iteration path was not sorted or
  normalized.

## What not to change without an ADR

- Do not add graph, collector, or reducer dependencies.
- Do not make this package read files or inspect repository state.
