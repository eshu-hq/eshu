// Package dart parses Dart source evidence for the parent parser engine.
//
// Parse reads one Dart source file through shared.ReadSource and emits the
// legacy parser payload buckets for imports, declarations, variables, function
// calls, and bounded dead-code root metadata. ParseWithParser and
// PreScanWithParser let the parent engine supply its runtime-owned tree-sitter
// parser, while Parse and PreScan remain package-local convenience entrypoints.
// Declaration annotations are consumed at the declaration boundary so class
// annotations do not leak into method decorators, and constructor declarations
// come only from class-member syntax so constructor calls stay call evidence.
package dart
