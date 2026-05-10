// Package rust parses Rust source files into Eshu parser payloads.
//
// Parse receives a caller-owned tree-sitter parser and emits deterministic
// buckets for functions, type declarations, modules, traits, impl blocks,
// expanded imports, constants, statics, type aliases, macro definitions and
// invocations, calls, selected dead-code root evidence, attributes, derives,
// conditional derives, nested field and enum-variant annotations, generic
// parameter evidence, impl-context evidence, and structured where-clause
// evidence. Bare Rust main
// roots require a Cargo entrypoint path such as src/main.rs, src/bin, examples,
// or build.rs, or direct runtime macro evidence, so library functions named
// main do not become roots by name alone. Public API roots require exact pub
// visibility, and benchmark roots require file-local Criterion identifier
// targets or direct benchmark attributes; generated or expression-based
// benchmark targets remain raw macro evidence. Trait implementation methods,
// including unsafe impl blocks, carry rust.trait_impl_method root evidence with
// their trait context so dead-code callers can keep runtime-dispatched trait
// surfaces conservative. Module declaration path
// candidates stay relative to the current file directory instead of probing the
// filesystem, except explicit path attributes, which replace the candidate list
// with the declared path; ResolveModuleRowFileCandidates exposes the same
// candidate calculation without filesystem probing. A bounded Cargo.toml helper
// scans package names, workspace members, feature names, default feature
// members, and target cfg dependency sections for later cfg resolution work,
// while ignoring dynamic TOML instead of guessing. The parent parser engine may
// probe repo-bounded module candidates and attach bounded
// module_resolution_status metadata when parsing a concrete repo path. Existing
// files outside the repo root are not treated as resolved modules. Item
// attributes may be
// multiline or share the item line, while nested field and enum-variant
// attributes stay on owned annotation rows. Cfg-gated items and macro-origin
// module/import rows name exactness blockers so dead-code callers can explain
// why Rust remains derived. Impl targets keep the receiver type and trim any
// trailing where clause. PreScan derives repository symbol names from the same
// payload path so parent parser pre-scan and full parse agree. The package
// preserves raw attributes and generic clauses as evidence without inferring
// reachability from arbitrary macro expansion, derives, conditional attributes,
// Cargo feature selection, or cfg evaluation.
package rust
