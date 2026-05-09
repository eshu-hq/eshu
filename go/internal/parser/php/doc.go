// Package php parses PHP source evidence for the parent parser engine.
//
// Parse reads one PHP source file through shared.ReadSource and emits the
// legacy parser payload buckets for classes, traits, interfaces, functions,
// imports, variables, calls, and trait-use adaptations. PreScan returns
// declaration names from the same payload path so repository pre-scan and full
// parse stay aligned. The package is deterministic and depends only on shared
// parser helpers.
package php
