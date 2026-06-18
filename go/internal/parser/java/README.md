# Java Parser

## Purpose

This package owns the Java tree-sitter adapter and Java metadata extraction
used by the parent parser engine. It emits source payload buckets for Java
classes, interfaces, annotations, enums, functions, variables, imports, calls,
dead-code roots, reflection references, static metadata class references, and
opt-in shared value-flow buckets for the supported Java taint subset.

## Ownership boundary

The package is responsible for Java syntax traversal, Java-specific type and
call inference, Java dead-code root classification, Java reflection evidence,
Java taint lowering over Spring request parameters and typed JDBC/JPA sinks,
same-file Java summary/interprocedural extraction, and ServiceLoader or Spring
metadata class references. The parent
internal/parser package owns registry dispatch, Engine methods, runtime
language lookup, and compatibility wrappers such as `parseJavaMetadata`.

## Exported surface

The godoc contract is in `doc.go`. Current exports are `Parse`, `PreScan`,
`ParseMetadata`, `ClassReference`, and `MetadataClassReferences`.

## Dependencies

The package imports `go/internal/parser/shared` for `Options`, file reads,
payload helpers, and tree-sitter node helpers. The opt-in value-flow path uses
`go/internal/parser/cfg`, `go/internal/parser/dataflowemit`,
`go/internal/parser/interproc`, `go/internal/parser/summary`,
`go/internal/parser/taint`, and `go/internal/parser/valueflow` so Java emits
the same payload schema as Go, Python, and JS/TS. It imports tree-sitter only
for the caller-owned parser and node traversal. It must not import the parent
`go/internal/parser` package, collector packages, graph storage, or reducer
code.

## Telemetry

This package emits no telemetry directly. Parser runtime timing and error
reporting remain owned by the parent parser engine and the collector surfaces
that call it.

No-Observability-Change: Java value-flow emission is behind the existing
`Options.EmitDataflow` parser gate and adds no metric, span, log field, status
field, queue, graph write, runtime knob, or high-cardinality label. Operators
continue to diagnose parser cost through the existing collector parse timing
metric `eshu_dp_file_parse_duration_seconds` and parse-stage logs.

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

Method-reference target roots stay bounded to source evidence. A `this`
receiver, typed receiver variable, or unambiguous same-file declared type
receiver can mark the matching method as a dead-code root across class,
interface, enum, and record contexts; unknown or duplicate simple receiver
names are ignored.

Metadata extraction accepts repository-relative or absolute paths and
normalizes separators before matching metadata locations. Invalid or duplicate
class names are ignored, and Spring factories preserve the starting line number
for continued values.

Java taint support is intentionally conservative. A source requires a real
Spring import for `@RequestParam`, `@PathVariable`, or `@RequestBody`; a
same-named local annotation is ignored. A SQL sink requires receiver type
evidence from the existing Java call-inference index plus the matching
`java.sql`, `jakarta.persistence`, or `javax.persistence` import or qualified
type for JDBC `Statement` / `PreparedStatement` or JPA `EntityManager`;
same-named local helper classes do not match.

Java value-flow summaries use stable `RepositoryID`, Java package name, class
context, and method signature identities so overloaded methods do not collide.
The per-file interprocedural pass only resolves same-class calls with bare
method names or `this.` receivers. Calls with unresolved argument types are
resolved only when the class has a single same-name/same-arity candidate; this
keeps unknown overloads as false negatives instead of inventing false edges.
Durable `dataflow_summaries` and `dataflow_sources` rows are omitted without
both repository and package identity, matching the stable identity contract.

No-Regression Evidence: `go test ./internal/parser -run
'TestJava(DataflowOffIsByteIdentical|TaintSpringRequestParamToJDBCSink|TaintIgnoresSameNamedLocalAnnotationAndSink|InterprocSummariesAndSources)'
-count=1` failed before Java emitted value-flow buckets, before sink matching
required import evidence, and before Java emitted summaries/sources/interproc
rows, then passed after the Java lowering and catalog were added. The gate-off
test proves default Java payloads remain byte-identical except for the opt-in
value-flow buckets when the gate is enabled.

## Related docs

- `docs/public/reference/local-testing.md`
- `docs/public/architecture.md`
