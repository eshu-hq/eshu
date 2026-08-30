# DBTSQL Parser Audit

## Overview
Extracts bounded column lineage from compiled dbt model SQL strings. This is a **declarative lineage** parser — it does NOT parse SQL grammar. It uses regex to identify SELECT projections, CTEs, FROM/JOIN relations, column aliases, and a bounded set of transform functions. 5 src files, 3 test files (the test count is the number of distinct test files in this package's directory cited under Verified-by-Test below, not every test file in the directory). All regex via `regexp.MustCompile` (4 files).

## Claimed Constructs
From `doc.go`, `README.md`, `lineage.go`:
- **Column lineage**: output column, source columns (fully qualified), transform kind, transform expression
- **CTE extraction**: Common Table Expressions are parsed and registered as relation bindings for downstream CTE references
- **Relation binding**: FROM/JOIN clauses resolved to column lists (supplied by callers from dbt manifest)
- **Supported transforms**: upper, lower, cast, date_trunc, concat, md5, coalesce, nullif, window functions (sum over), +
- **Unresolved references**: expressions outside the bounded set produce unresolved entries
- **Projection count**: number of output columns in the final SELECT
- **Safe wrapper functions**: `dbt_utils.identity()` recognized as lineage-preserving (passes source columns through)

## Verified-by-Test Constructs
- `TestExtractCompiledModelLineageCapturesMacroProjectionWithoutUnresolvedGap` (`lineage_test.go:11`): dbt_utils.identity() wrapper
- `TestExtractCompiledModelLineageCapturesWindowProjectionWithoutUnresolvedGap` (`lineage_test.go:42`): window_sum over columns
- `TestExtractCompiledModelLineageCapturesNestedSafeWrapperWithoutUnresolvedGap` (`lineage_test.go:79`): upper(coalesce(...))
- `TestExtractCompiledModelLineageCapturesNestedWrapperOnCTEColumnWithoutUnresolvedGap` (`lineage_test.go:112`): CTE with downstream transform
- `TestExtractCompiledModelLineage_ParitySupportedTransforms` (`lineage_parity_test.go:11`): cast, date_trunc, concat, upper over macro, md5 table-driven parity
- `json_dbt_test.go` (external `dbtsql_test` package): drives the parent parser
  Engine over a dbt manifest and asserts model, source, and macro rows,
  `COMPILES_TO`/`USES_MACRO`/`COLUMN_DERIVES_FROM` edges, manifest-level
  wildcard expansion, `coalesce` transform metadata, and complete coverage
  state. It does guard this package's extractor: mutating the `coalesce`
  branch of `expression_helpers.go` turns it red. It reaches that code
  through the parent Engine and the caller-supplied callback rather than
  calling `ExtractCompiledModelLineage` directly, so it guards the
  integrated path, not the extractor's unit behavior.

## Unverified Constructs / Coverage Gaps
- **Relation binding from caller-supplied column lists**: tested only with explicit column maps; behavior when relation has no known columns is not directly tested
- **nullif**: listed under Claimed Constructs above, but no code in this package
  implements it — `rg -i nullif go/internal/parser/dbtsql/` finds nothing — so there
  is no behavior to test. The Claimed Constructs entry is the thing to correct.
- **Direct transform parity rows**: lower is covered inside the nested CTE test and coalesce is covered by nested and integrated Engine tests, but neither has its own parity-table row
- **Multiple CTE scenarios** (CTE referencing another CTE, CTE chains deeper than 1 level)
- **Unresolved reasons without a test**: `TestExtractCompiledModelLineage_ParityUnresolvedReasons` (`lineage_parity_test.go:211`) does feed unsupported expressions and assert the entry that appears, but it reaches only three of the eleven reasons this package emits — `templated_expression_not_resolved`, `macro_expression_not_resolved`, and `unqualified_column_reference_ambiguous`. The other eight have no test: `aggregate_expression_semantics_not_captured`, `derived_expression_semantics_not_captured`, `multi_input_expression_semantics_not_captured`, `window_expression_semantics_not_captured` (`expressions.go:36-41`), and `source_alias_not_resolved`, `wildcard_projection_not_supported`, `cte_column_not_resolved`, `unqualified_column_reference_not_resolved` (`lineage.go:342-398`)
- **dbt_utils.star()** or other dbt helpers beyond identity
- **Subqueries in FROM** — only table references tested

## Edge Cases Considered
- dbt_utils.identity() macro recognized as lineage-preserving
- Window functions (sum over) tracked with all partition/order columns
- Nested transforms (upper over coalesce, trim over lower)
- CTE → final SELECT column chaining

## Edge Cases NOT Considered
- Qualified star (`alias.*`) over a relation with **no** known columns — the
  `wildcard_projection_not_supported` branch in `resolveQualifiedReference`
  (`lineage.go:346`). Qualified star expansion over a relation that *does*
  have known columns is tested; see the Verdict.
- Bare `select *` — dropped entirely: no lineage row **and** no unresolved
  entry. It reaches neither wildcard branch, because
  `dbtQualifiedReferenceScanRe` requires an `alias.` prefix and
  `dbtIdentifierRe` (`identifiers.go:12`) requires `[A-Za-z_]`, so `*` is
  never an unqualified identifier; `expressionHonestyGapReason("*")` returns
  `""`. That silence is a gap against this package's own
  preserve-unresolved-reasons invariant in `AGENTS.md`, and it is untested.
- UNION/UNION ALL queries
- Correlated subqueries
- LATERAL joins
- Table-valued function calls in FROM
- Schema-qualified relation names beyond three-part names
- Whitespace-only or empty compiled SQL
- Jinja residue in compiled SQL (dbt generates clean SQL, but templated refs are possible)

## Verdict
**moderate** — The parity table is table-driven with ten supported-transform cases (cast, date_trunc, concat, upper over a lineage-preserving macro, md5, concat_ws, a macro-heavy wrapper, a templated macro wrapper, case with multiple sources, and arithmetic with multiple sources) plus four unresolved-reason cases. Separate nested and integrated tests cover lower and coalesce, neither with its own parity row. CTE chaining and nested transforms are tested, and so are unresolved references and qualified `select *` expansion — the parity table asserts the expression and reason for each of its four unresolved cases, and `json_dbt_test.go` drives `o.*` expansion through the Engine. What is missing is the other eight unresolved reasons, including the `wildcard_projection_not_supported` branch a wildcard over a relation with no known columns takes. As a permanent exception that extracts declarative lineage from compiled SQL (not source grammar), moderate coverage is appropriate.

## Recommended Actions
- Extend the unresolved-reason parity table to the eight reasons listed above that no test reaches
- Add parity-table rows for `lower` and `coalesce`
- Document that DBTSQL is a **permanent exception** — it uses bounded regex lineage scanning, not tree-sitter grammar
- Consider a test with multiple CTE levels (CTE referencing another CTE)
