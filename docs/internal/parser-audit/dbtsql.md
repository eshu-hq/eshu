# DBTSQL Parser Audit

## Overview
Extracts bounded column lineage from compiled dbt model SQL strings. This is a **declarative lineage** parser — it does NOT parse SQL grammar. It uses regex to identify SELECT projections, CTEs, FROM/JOIN relations, column aliases, and a bounded set of transform functions. 5 src files, 2 test files. All regex via `regexp.MustCompile` (4 files).

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

## Unverified / Claimed-but-Untested Constructs
- **Relation binding from caller-supplied column lists**: tested only with explicit column maps; behavior when relation has no known columns is not directly tested
- **lower, coalesce, nullif**: claimed in expressions.go but not explicitly in parity test; code patterns exist but no dedicated test
- **Multiple CTE scenarios** (CTE referencing another CTE, CTE chains deeper than 1 level)
- **Unresolved references**: the UnresolvedReferences slice is checked for length 0 in tests, but no test deliberately feeds an unsupported expression and asserts an unresolved entry appears
- **dbt_utils.star()** or other dbt helpers beyond identity
- **Subqueries in FROM** — only table references tested

## Edge Cases Considered
- dbt_utils.identity() macro recognized as lineage-preserving
- Window functions (sum over) tracked with all partition/order columns
- Nested transforms (upper over coalesce, trim over lower)
- CTE → final SELECT column chaining

## Edge Cases NOT Considered
- Star projections (SELECT *)
- UNION/UNION ALL queries
- Correlated subqueries
- LATERAL joins
- Table-valued function calls in FROM
- Schema-qualified relation names beyond three-part names
- Whitespace-only or empty compiled SQL
- Jinja residue in compiled SQL (dbt generates clean SQL, but templated refs are possible)

## Verdict
**moderate** — The parser correctly handles the bounded supported transform set with table-driven parity tests for each transform kind. CTE chaining and nested transforms are tested. However, deliberately provoking unresolved references and testing edge cases like SELECT * are missing. As a permanent exception that extracts declarative lineage from compiled SQL (not source grammar), moderate coverage is appropriate.

## Recommended Actions
- Add a test that deliberately feeds an unsupported expression and asserts UnresolvedReferences is populated
- Add a test for `lower`, `coalesce`, `nullif` in the parity test table
- Document that DBTSQL is a **permanent exception** — it uses bounded regex lineage scanning, not tree-sitter grammar
- Consider a test with multiple CTE levels (CTE referencing another CTE)
