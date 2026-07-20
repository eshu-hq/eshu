# Dart Parser

## Purpose

This package owns the Dart parser adapter used by the parent parser engine. It
uses tree-sitter syntax for import/export names, class-style declarations,
function rows, variables, call sites, and Dart/Flutter dead-code root
metadata, emitting call evidence in the legacy `function_calls` payload
shape.

## Ownership boundary

The package is responsible for Dart source scanning and payload bucket
population. The parent parser package still owns registry dispatch, engine
orchestration, repo path handling, and parse telemetry.

## Exported surface

The godoc contract is in doc.go. Current exports are Parse, ParseWithParser,
PreScan, and PreScanWithParser. The parent engine uses the `WithParser`
variants so Dart shares the same runtime-owned tree-sitter parser lifecycle as
other grammar-backed adapters.

## Dependencies

This package imports the Go standard library, internal/parser/shared,
github.com/tree-sitter/go-tree-sitter, and the Dart grammar binding. It must
not import the parent internal/parser package.

## Telemetry

This package emits no telemetry. Parse timing remains owned by the parent
parser engine.

## Gotchas / invariants

Bucket ordering must stay deterministic because parser payloads become fact
inputs. PreScan returns function and class names using the same payload path as
Parse, then sorts those names before returning them to the parent engine.
`dead_code_root_kinds` is limited to syntax-local evidence: top-level `main`,
constructors, `@override`, Flutter `build`/`createState`, and public `lib/`
declarations outside `lib/src/`. Annotations attached to class declarations are
consumed at the declaration boundary so they do not become member decorators.
Constructor detection comes from constructor signature nodes inside class
bodies; constructor calls inside method bodies must remain call evidence, not
constructor declarations. `function_calls` rows come from AST call-site
detection (`calls.go`, `dartCallChain.observe`) over call-expression grammar
shapes (`selector`/`argument_part`, cascades, `new`/`const` object creation),
not a byte scan, so a declaration signature can never be misclassified as a
call site (eshu-hq/eshu#5332). Call detection is folded into the single
`dartSyntaxIndex.collect` traversal (eshu-hq/eshu#5350) rather than run as a
separate second full tree walk; collect uses calls-only descent into signature
subtrees so parameter-default and annotation call sites are captured without
emitting spurious declaration rows. Rows carry both `name` (bare callee) and
`full_name` (dotted qualified chain, e.g. `f.build`), deduplicated by
`full_name` so distinct receivers sharing a method name (`a.foo()` vs
`b.foo()`) both survive. Import collection must avoid double-counting
`import_or_export` wrapper nodes and their concrete library children, and it
marks Dart `library_import` rows with `import_type=import` and
`library_export` rows with `import_type=export` so downstream resolvers do not
treat export-only barrel files as local lexical imports.

## Related docs

- docs/public/languages/support-maturity.md
