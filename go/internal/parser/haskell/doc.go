// Package haskell parses simple Haskell source evidence for the parent parser engine.
//
// Parse reads one Haskell source file through shared.ReadSource and emits the
// legacy parser payload buckets for modules, imports, data/class declarations,
// functions, bounded function calls from definition bodies, continuation lines,
// and indented keyword-led bindings such as let expressions. Where-block local
// bindings stay in the variables bucket rather than becoming top-level
// functions. Parse annotates explicit module exports, main functions, typeclass
// methods, and instance methods with dead-code root metadata. PreScan returns
// declaration names from the same payload path. The package is deterministic and
// depends only on shared parser helpers.
package haskell
