// Package elixir owns Elixir parser extraction without depending on the parent
// parser dispatcher.
//
// Parse emits modules, protocols, functions, imports, attributes, variables,
// bounded call metadata, dead-code root kinds, observed dynamic-dispatch
// blockers, and Hex dependency evidence from Mix manifests and lockfiles.
// Tree-sitter supplies syntax-aware function spans, multiline signatures,
// decorators, and module context while bounded helpers preserve existing call
// and dependency contracts. Root metadata stays conservative: Application start
// needs Application syntax, and OTP/Phoenix callback roots use arity checks
// where the framework contract defines them. Hex dependency rows admit literal
// registry dependencies only; VCS dependencies stay provenance-only so
// downstream reducers do not invent package consumption. PreScan returns the
// deterministic names used by the parent parser's repository import-map pass.
// ParseWithParser and PreScanWithParser let the parent engine reuse a
// caller-owned runtime parser without importing parser dispatcher internals. The
// implementation stays parent-independent so Elixir-specific helpers do not add
// more surface area to the central parser dispatcher.
package elixir
