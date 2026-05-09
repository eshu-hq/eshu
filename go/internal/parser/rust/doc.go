// Package rust parses Rust source files into Eshu parser payloads.
//
// Parse receives a caller-owned tree-sitter parser and emits deterministic
// buckets for functions, type declarations, traits, impl blocks, imports,
// macro invocations, calls, and lifetime evidence. PreScan derives repository
// symbol names from the same payload path so parent parser pre-scan and full
// parse agree. The package intentionally preserves some Rust syntax, such as
// brace imports, as raw evidence until a downstream contract needs a more
// structured shape.
package rust
