// Package swift owns Swift parser extraction without depending on the parent
// parser dispatcher.
//
// Parse emits Swift imports, nominal types, functions, variables, call
// metadata, and parser-backed dead-code root kinds from source text. The
// tree-sitter AST node walk is the sole extraction path: a first pass records
// whole-file semantic facts (conformances, protocol methods, Vapor route
// handlers) and a second pass emits every payload row from AST nodes, so
// multiline declarations, attributes, generics, and nested call arguments are
// read from grammar structure rather than line-scan regex. PreScan returns
// deterministic names for the parent parser's repository import-map pass. The
// implementation stays parent-independent so Swift-specific heuristics can
// change without widening the central parser dispatcher.
package swift
