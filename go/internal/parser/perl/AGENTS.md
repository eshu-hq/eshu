# AGENTS.md - internal/parser/perl guidance

## Read first

1. README.md - package boundary and legacy payload shape
2. doc.go - godoc contract for the Perl adapter
3. parser.go - regex parser and pre-scan behavior
4. parser_test.go - behavior coverage for payload shape

## Invariants this package enforces

- Dependency direction stays one way: parent parser code may import this
  package, but this package must not import internal/parser.
- Parse preserves package declarations as class rows.
- Public packages and bounded Exporter declarations emit
  `dead_code_root_kinds` metadata for the query dead-code policy.
- Perl special blocks are modeled as derived roots, not ordinary callable
  subroutines.
- PreScan derives names from Parse so parent pre-scan and full parse agree.

## Common changes and how to scope them

- Add Perl evidence by writing a focused test in parser_test.go first.
- Keep registry, Engine dispatch, and content-shape changes outside this
  package unless the task explicitly includes those files.
- Use internal/parser/shared helpers for payload buckets and sorting.

## Failure modes and how to debug

- Missing package rows usually mean the package regex no longer accepts
  namespace separators.
- Missing call rows usually mean the call regex filtered a line shape that
  parent parser tests rely on.
- Dead-code false positives around `main`, `new`, `@EXPORT`, `@EXPORT_OK`,
  `AUTOLOAD`, `DESTROY`, or special blocks usually mean parser metadata did not
  survive into the content entity row.

## Anti-patterns specific to this package

- Importing the parent parser package.
- Changing package rows from classes to a new bucket without downstream shape
  work.
- Adding repository-specific Perl conventions without fixture evidence.

## What NOT to change without an ADR

- Do not change Perl extension ownership or registry behavior from this
  package.
