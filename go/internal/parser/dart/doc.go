// Package dart parses simple Dart source evidence for the parent parser engine.
//
// Parse reads one Dart source file through shared.ReadSource and emits the
// legacy parser payload buckets for imports, declarations, variables, function
// calls, and bounded dead-code root metadata. PreScan returns the names the
// parent engine needs for repository import context. Declaration annotations are
// consumed at the declaration boundary so class annotations do not leak into
// method decorators. The package is deterministic and depends only on shared
// parser helpers, not the parent parser package.
package dart
