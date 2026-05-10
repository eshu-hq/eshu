// Package rust parses Rust source files into Eshu parser payloads.
//
// Parse receives a caller-owned tree-sitter parser and emits deterministic
// buckets for functions, type declarations, modules, traits, impl blocks,
// expanded imports, constants, statics, type aliases, macro definitions and
// invocations, calls, selected dead-code root evidence, attributes, derives,
// and generic parameter evidence. Bare Rust main roots require a Cargo
// entrypoint path such as src/main.rs, src/bin, examples, or build.rs, or direct
// runtime macro evidence, so library functions named main do not become roots
// by name alone. Public API roots require exact pub visibility, and benchmark
// roots require file-local Criterion identifier targets or direct benchmark
// attributes; generated or expression-based benchmark targets remain raw macro
// evidence. Module declaration path candidates stay relative to the current file
// directory instead of probing the filesystem. Item attributes may be multiline
// or share the item line, but nested field and enum-variant attributes are not
// promoted to parent metadata. Impl targets keep the receiver type and trim any
// trailing where clause.
// PreScan derives repository symbol names from the same payload path so parent
// parser pre-scan and full parse agree. The package preserves raw attributes
// and generic clauses as evidence without inferring reachability from arbitrary
// macros, derives, or conditional attributes.
package rust
