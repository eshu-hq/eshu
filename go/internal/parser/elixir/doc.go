// Package elixir owns Elixir parser extraction without depending on the parent
// parser dispatcher.
//
// Parse emits modules, protocols, functions, imports, attributes, variables,
// bounded call metadata, dead-code root kinds, observed dynamic-dispatch
// blockers, and Hex dependency evidence from Mix manifests and lockfiles.
// Every Elixir source symbol is extracted from the tree-sitter AST: module,
// function, import, attribute, and call rows are keyed by node spans rather than
// a text line index, so module membership, end lines, and per-clause context
// follow the parse tree. Root metadata stays conservative: Application start
// needs Application syntax, and OTP/Phoenix callback roots use arity checks
// where the framework contract defines them. Hex dependency rows from
// mix.exs and mix.lock remain manifest parsing, the documented structured-format
// exception, and are not Elixir source symbols. Hex dependency rows admit literal
// registry dependencies only; VCS dependencies stay provenance-only so
// downstream reducers do not invent package consumption. PreScan returns the
// deterministic names used by the parent parser's repository import-map pass.
// ParseWithParser and PreScanWithParser let the parent engine reuse a
// caller-owned runtime parser without importing parser dispatcher internals. The
// implementation stays parent-independent so Elixir-specific helpers do not add
// more surface area to the central parser dispatcher.
package elixir
