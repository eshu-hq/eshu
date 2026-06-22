# AGENTS.md - internal/parser/java guidance

## Read first

1. `README.md` - package boundary, exported surface, and invariants
2. `doc.go` - godoc contract for `Parse`, `PreScan`, `ParseMetadata`,
   `ClassReference`, and `MetadataClassReferences`
3. `parser.go` - payload assembly, declaration traversal, imports, and calls
4. `call_inference.go`, `call_context.go`, and `type_inference_helpers.go` -
   Java receiver, argument, class-context, and return-type inference
5. `dead_code_roots.go` and `reflection.go` - Java root classification and
   literal reflection references
6. `metadata.go` and `parser_metadata.go` - ServiceLoader, Spring metadata,
   decorators, parameter types, and method-reference target evidence

## Invariants this package enforces

- Dependency direction stays one way: parent parser code may import this
  package, but this package must not import `go/internal/parser`.
- `Parse` preserves the parent payload contract for `functions`, `classes`,
  `interfaces`, `annotations`, `enums`, `variables`, `imports`, and
  `function_calls`; `ParseMetadata` preserves the `java_metadata`
  compatibility payload.
- The caller owns the tree-sitter parser and must configure it for Java before
  calling `Parse` or `PreScan`.
- Local variables are emitted only when `Options.VariableScope` normalizes to
  `all`; module scope remains the default.
- Reflection and metadata evidence stay static. Dynamic strings and invalid
  class names must not become graph evidence.
- Ordering follows source line, then name, through shared bucket sorting.

## Common changes and how to scope them

- Add Java syntax payload fields in `parser.go` with a parent engine test first
  when the contract is visible through Engine ParsePath.
- Add receiver or argument inference in `call_inference.go`,
  `call_context.go`, or `type_inference_helpers.go` with a child-package unit
  test when the helper contract is internal.
- Add dead-code roots in `dead_code_roots.go` with positive and negative parent
  parser tests so ordinary methods do not become roots by name alone.
- Add reflection support in `reflection.go` only for literal, statically named
  evidence.
- Add a metadata file shape by extending `metadata.go` and `metadata_test.go`
  first.

## Failure modes and how to debug

- Missing Java symbols usually mean the tree-sitter kind changed or the walk in
  `parser.go` missed a declaration kind.
- Missing receiver types usually mean the declaration was outside the scope
  indexed by `buildJavaCallInferenceIndex`.
- Extra dead-code roots usually mean annotation, framework, or hook matching in
  `dead_code_roots.go` accepted a broad shape.
- Missing ServiceLoader roots usually mean the path classifier in `metadata.go`
  did not match the resource path.
- Wrong line numbers usually mean helper code used the enclosing node instead
  of the declaration or literal node that proves the evidence.

## Anti-patterns specific to this package

- Importing the parent parser package to reuse private helpers.
- Adding backend, collector, reducer, or graph storage dependencies.
- Emitting graph truth from dynamic Java strings, comments, or naming
  conventions without source evidence.
- Keeping compatibility wrappers in this package; parent Engine signatures
  remain in `go/internal/parser/java_language.go`.

## What NOT to change without an ADR

- Do not change the Java payload bucket names or parent Engine method
  signatures.
- Do not change Java dead-code root semantics without fixture evidence and
  query-surface impact review.
- Do not add runtime telemetry directly here unless the parser telemetry
  contract moves out of the parent engine.

## Permanent Regex Exception: metadataClassNamePattern

**Location:** `metadata.go` — constant `javaMetadataClassNameExpression`,
variable `metadataClassNamePattern`, called only from `appendMetadataClassName`.

**Regex:** `^[A-Za-z_$][\w$]*(\.[A-Za-z_$][\w$]*)+$`

**Classification: content-classification over bounded plain-text resource
files. This is NOT symbol extraction from a Java AST. Migration to tree-sitter
is not applicable.**

### Why no tree-sitter grammar applies

`MetadataClassReferences` and `ParseMetadata` operate exclusively on
non-Java resource files:

- `META-INF/services/<interface-name>` — ServiceLoader provider lists.
  Each line is a plain-text fully-qualified class name, not Java source.
- `META-INF/spring.factories` — Spring Boot 1/2 auto-configuration keys.
  Each line is a Java properties `key=value` entry; values are
  comma-separated class-name tokens. No Java grammar applies.
- `META-INF/spring/org.springframework.boot.autoconfigure.AutoConfiguration.imports`
  — Spring Boot 3 auto-configuration list. One class name per line,
  plain text. No Java grammar applies.

The tree-sitter Java grammar models Java *source* syntax (class bodies,
method declarations, expressions, etc.). It has no grammar for these
property-style or newline-delimited class-name list formats. There is no
AST node to walk; the only structured content is the dotted class-name
token itself, which is what the regex validates.

All genuine Java *source* symbol extraction (classes, methods, interfaces,
imports, function calls) lives in `parser.go`, `parser_metadata.go`, and
supporting files, driven by tree-sitter. The `metadataClassNamePattern`
regex has zero overlap with that path.

### What the pattern guards

The regex enforces the following invariants before a candidate string
becomes graph evidence:

1. Must start with a letter, underscore, or `$` (Java identifier start
   characters) — rejects comment fragments, numeric lines, blank lines, and
   property keys.
2. Must contain at least one `.` separator — rejects bare single-segment
   identifiers, Spring properties keys, and accidental plain-text values.
3. Each segment must be a valid Java identifier — rejects hyphens, spaces,
   dynamic `${placeholder}` expressions, and other non-class tokens.
4. Duplicates are deduplicated per file by `appendMetadataClassName` before
   the pattern check, so the regex never runs on already-seen candidates.

Invalid, dynamic, duplicate, or unsupported lines are silently dropped
rather than becoming graph evidence. This is the intended contract.

### Why this is a permanent exception

Agents auditing this package MUST NOT migrate `metadataClassNamePattern` to
a tree-sitter query. The correct action for any future grammar-applicable
site is to write a new tree-sitter-based extractor. The metadata regex must
be preserved exactly as-is unless the entire `MetadataClassReferences`
surface is replaced by a purpose-built parser for properties/list formats —
which would not be tree-sitter Java either.

Do not remove or weaken the pattern. Weakening it risks emitting dynamic
strings, comment fragments, or property keys as graph evidence.

### No-Regression Evidence

Verification run after adding characterization tests (6 test functions in
`metadata_test.go`):

```
cd go && gofmt -l internal/parser/java   # empty — no formatting drift
cd go && go test ./internal/parser/... -count=1
# 1170 passed, 41 packages
cd go && go test ./internal/parser/java/... -count=1 -run TestMetadata -v
# 6 passed: TestMetadataClassReferences,
#           TestMetadataClassReferencesRejectsDynamicOrInvalidNames,
#           TestMetadataClassReferencesMetaInfServicesProvider,
#           TestMetadataClassReferencesSpringAutoconfigurationImports,
#           TestMetadataClassReferencesUnrecognizedPath,
#           TestMetadataClassReferencesRejectsBareSingleSegment
```

### No-Observability-Change

No runtime behavior was modified. No telemetry, spans, metrics, or log
lines were added or removed. This change adds test coverage and
documentation only.
