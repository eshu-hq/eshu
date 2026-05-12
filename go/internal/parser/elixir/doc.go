// Package elixir owns Elixir parser extraction without depending on the parent
// parser dispatcher.
//
// Parse emits modules, protocols, functions, imports, attributes, variables,
// bounded call metadata, dead-code root kinds, and observed dynamic-dispatch
// blockers from function bodies and one-line declarations in Elixir source
// text. Root metadata stays conservative: Application start needs Application
// syntax, and OTP/Phoenix callback roots use arity checks where the framework
// contract defines them. PreScan returns the deterministic names used by the
// parent parser's repository import-map pass. The implementation stays
// parent-independent so Elixir-specific helpers do not add more surface area to
// the central parser dispatcher.
package elixir
