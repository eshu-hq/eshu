// Package haskell parses simple Haskell source evidence for the parent parser engine.
//
// Parse reads one Haskell source file and emits the legacy parser payload
// buckets for modules, imports, data/newtype/type/class declarations, functions,
// and bounded function calls. Primary symbol extraction walks the tree-sitter
// Haskell grammar: the module header, data/newtype/type and data-family
// declarations, typeclasses, instances, class-method type signatures, and
// top-level value bindings are resolved from grammar nodes through
// ParseWithParser rather than a line scan. Two bounded textual-evidence readers
// remain by design: a where-block scan that records simple local bindings as
// variables (keeping them out of the functions bucket), and a lexical token scan
// over definition right-hand sides that records function-call evidence. Imports
// use a bounded import-line reader that normalizes safe, qualified, and
// package-qualified forms. Parse annotates explicit module exports, main
// functions, typeclass methods, and instance methods with dead-code root
// metadata. PreScan returns declaration names from the same payload path.
package haskell
