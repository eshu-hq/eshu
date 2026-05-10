// Package rust parses Rust source files into Eshu parser payloads.
//
// Parse receives a caller-owned tree-sitter parser and emits deterministic
// buckets for functions, type declarations, modules, traits, impl blocks,
// expanded imports, constants, statics, type aliases, macro definitions and
// invocations, calls, selected dead-code root evidence, attributes, derives,
// conditional derives, nested field and enum-variant annotations, generic
// parameter evidence, and structured where-clause evidence. Bare Rust main
// roots require a Cargo entrypoint path such as src/main.rs, src/bin, examples,
// or build.rs, or direct runtime macro evidence, so library functions named
// main do not become roots by name alone. Public API roots require exact pub
// visibility, and benchmark roots require file-local Criterion identifier
// targets or direct benchmark attributes; generated or expression-based
// benchmark targets remain raw macro evidence. Module declaration path
// candidates stay relative to the current file
// directory instead of probing the filesystem, except explicit path attributes,
// which replace the candidate list with the declared path. Item attributes may
// be multiline or share the item line, while nested field and enum-variant
// attributes stay on owned annotation rows. Impl targets keep the receiver type
// and trim any trailing where clause.
// PreScan derives repository symbol names from the same payload path so parent
// parser pre-scan and full parse agree. The package preserves raw attributes
// and generic clauses as evidence without inferring reachability from arbitrary
// macro expansion, derives, or conditional attributes.
package rust
