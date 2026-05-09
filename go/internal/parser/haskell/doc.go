// Package haskell parses simple Haskell source evidence for the parent parser engine.
//
// Parse reads one Haskell source file through shared.ReadSource and emits the
// legacy parser payload buckets for modules, imports, data/class declarations,
// functions, and where-block variables. PreScan returns declaration names from
// the same payload path. The package is deterministic and depends only on
// shared parser helpers.
package haskell
