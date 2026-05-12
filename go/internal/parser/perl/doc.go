// Package perl parses simple Perl source evidence for the parent parser engine.
//
// Parse reads one Perl source file through shared.ReadSource and emits the
// legacy parser payload buckets for packages, imports, subroutines, variables,
// and function calls. It also marks bounded dead-code roots for public
// packages, Exporter declarations, script entrypoints, constructors, special
// blocks, AUTOLOAD, and DESTROY. PreScan returns declaration names from the
// same payload path. The package is deterministic and depends only on shared
// parser helpers.
package perl
