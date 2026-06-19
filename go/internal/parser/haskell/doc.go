// Package haskell parses simple Haskell source evidence for the parent parser engine.
//
// Parse reads one Haskell source file and emits the legacy parser payload
// buckets for modules, imports, data/class declarations, functions, bounded
// function calls from definition bodies, continuation lines, and indented
// keyword-led bindings such as let expressions. Tree-sitter supplies
// syntax-aware function spans and names through ParseWithParser, while bounded
// helpers preserve call and local-variable contracts. Where-block local
// bindings stay in the variables bucket rather than becoming top-level
// functions. Parse annotates explicit module exports, main functions,
// typeclass methods, and instance methods with dead-code root metadata. PreScan
// returns declaration names from the same payload path.
package haskell
