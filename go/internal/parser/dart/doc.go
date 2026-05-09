// Package dart parses simple Dart source evidence for the parent parser engine.
//
// Parse reads one Dart source file through shared.ReadSource and emits the
// legacy parser payload buckets for imports, declarations, variables, and
// function calls. PreScan returns the names the parent engine needs for
// repository import context. The package is deterministic and depends only on
// shared parser helpers, not the parent parser package.
package dart
