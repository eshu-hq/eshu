// Package perl parses simple Perl source evidence for the parent parser engine.
//
// Parse reads one Perl source file through shared.ReadSource and emits the
// legacy parser payload buckets for packages, imports, subroutines, variables,
// and function calls. PreScan returns declaration names from the same payload
// path. The package is deterministic and depends only on shared parser helpers.
package perl
