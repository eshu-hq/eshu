# Java Parser

## Purpose

This package owns the Java tree-sitter adapter and Java metadata extraction
used by the parent parser engine. It emits source payload buckets for Java
classes, interfaces, annotations, enums, functions, variables, imports, calls,
dead-code roots, reflection references, and static metadata class references.

## Ownership boundary

The package is responsible for Java syntax traversal, Java-specific type and
call inference, Java dead-code root classification, Java reflection evidence,
and ServiceLoader or Spring metadata class references. The parent
internal/parser package owns registry dispatch, Engine methods, runtime
language lookup, and compatibility wrappers such as `parseJavaMetadata`.

## Exported surface

The godoc contract is in `doc.go`. Current exports are `Parse`, `PreScan`,
`ParseMetadata`, `ClassReference`, and `MetadataClassReferences`.

## Dependencies

The package imports `go/internal/parser/shared` for `Options`, file reads,
payload helpers, and tree-sitter node helpers. It imports tree-sitter only for
the caller-owned parser and node traversal. It must not import the parent
`go/internal/parser` package, collector packages, graph storage, or reducer
code.

## Telemetry

This package emits no telemetry directly. Parser runtime timing and error
reporting remain owned by the parent parser engine and the collector surfaces
that call it.

## Gotchas / invariants

`Parse` expects the caller to provide a parser already configured for the Java
tree-sitter grammar. It preserves the parent payload shape and bucket ordering
contract through shared helpers.

`PreScan` returns Java type and method names from declarations only. It does
not infer dependency edges or walk metadata files.

`ParseMetadata` preserves the `java_metadata` payload shape used by the parent
registry while keeping ServiceLoader and Spring extraction in this package.

Reflection evidence is deliberately static: only literal class and method
references become `function_calls` rows. Dynamic strings remain absent so graph
reachability does not claim evidence the source did not prove.

Metadata extraction accepts repository-relative or absolute paths and
normalizes separators before matching metadata locations. Invalid or duplicate
class names are ignored, and Spring factories preserve the starting line number
for continued values.

## Related docs

- `docs/docs/reference/local-testing.md`
- `docs/docs/architecture.md`
