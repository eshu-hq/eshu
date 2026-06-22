// Package kotlin owns Kotlin parser extraction for the parent parser engine.
//
// Parse reads one Kotlin source file and returns the payload buckets consumed by
// collector and projector code: declarations, imports, variables, function
// calls, inferred receiver metadata, and parser-backed dead-code root metadata
// for bounded Kotlin entrypoint and framework callbacks. PreScan uses the same
// extraction path so import-map discovery sees the same function, class, and
// interface names as normal parsing.
//
// Extraction walks the tree-sitter Kotlin AST end to end. Declarations,
// imports, variables, qualified and bare (receiver-less) calls, receiver/type
// inference, smart-cast flow, scope functions, cast receivers, and
// package-bounded sibling return-type lookups are all derived from node kinds,
// ranges, and child relationships. Imported bare calls emit a call edge because
// Kotlin imports do not distinguish a function from a type; only file-local type
// declarations are treated as constructor targets. The parser holds no regular
// expressions and performs no line-scan symbol extraction; lexical scope is the
// AST nesting itself, so nested classes, companion objects, lambdas, and guarded
// blocks resolve receivers without brace-depth bookkeeping.
//
// No-Regression Evidence: `go test ./internal/parser -run Kotlin -count=1`,
// `go test ./internal/reducer -run Kotlin -count=1`, and
// `go test ./internal/parser/goldenaudit -count=1` pass unchanged after the
// regex/line-scan extraction was replaced by AST node-walking. The payload
// map[string]any keys and value shapes Kotlin emits are byte-identical to the
// prior hybrid parser; the AST walk adds one bounded tree traversal per file
// (plus the existing bounded sibling-file parse for return types) within the
// per-file parse budget measured by `eshu_dp_file_parse_duration_seconds`.
// No-Observability-Change: this parser-local change adds no metric, span, log,
// status field, queue behavior, graph query, environment variable, or runtime
// knob.
package kotlin
