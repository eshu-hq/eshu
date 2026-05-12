// Package ruby parses simple Ruby source evidence for the parent parser engine.
//
// Parse reads one Ruby source file through shared.ReadSource and emits the
// legacy parser payload buckets for modules, classes, methods, imports, module
// inclusions, variables, method calls, block end lines, and bounded dead-code
// root metadata. PreScan returns declaration names from the same payload path.
// The package keeps constants in the existing variable bucket and treats
// unmodeled framework DSL chains as bounded call evidence, not framework-root
// truth. The package is deterministic and depends only on shared parser helpers.
package ruby
