// Package kotlin owns Kotlin parser extraction for the parent parser engine.
//
// Parse reads one Kotlin source file and returns the payload buckets consumed by
// collector and projector code: declarations, imports, variables, function
// calls, inferred receiver metadata, and parser-backed dead-code root metadata
// for bounded Kotlin entrypoint and framework callbacks. PreScan uses the same
// extraction path so import-map discovery sees the same function, class, and
// interface names as normal parsing.
//
// No-Regression Evidence: multiline class/function syntax is indexed through
// the shared tree-sitter runtime before line-level semantic inference, so
// existing payload buckets stay stable while class scope and end-line metadata
// survive multiline declarations. No-Observability-Change: this parser-local
// extraction change adds no metric, span, log, status field, queue behavior,
// graph query, environment variable, or runtime knob.
package kotlin
