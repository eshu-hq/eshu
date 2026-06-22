// Package swift owns Swift parser extraction without depending on the parent
// parser dispatcher.
//
// Parse walks the Swift tree-sitter AST once and emits imports, nominal types,
// functions, variables, parser-backed dead-code root kinds, and bounded call
// metadata directly from node ranges. There is no line-scan fallback: every row
// is keyed by an AST span, so declaration ownership, multiline signatures, and
// call targets come from the grammar rather than trimmed source lines. PreScan
// returns deterministic names for the parent parser's repository import-map pass.
// The implementation stays parent-independent so Swift-specific heuristics can
// change without widening the central parser dispatcher.
package swift
