// Package ruby parses simple Ruby source evidence for the parent parser engine.
//
// Parse reads one Ruby source file through shared.ReadSource and emits the
// legacy parser payload buckets for modules, classes, methods, imports, module
// inclusions, variables, and method calls. PreScan returns declaration names
// from the same payload path. The package is deterministic and depends only on
// shared parser helpers.
package ruby
