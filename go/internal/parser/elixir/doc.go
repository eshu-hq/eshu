// Package elixir owns Elixir parser extraction without depending on the parent
// parser dispatcher.
//
// Parse emits modules, protocols, functions, imports, attributes, variables,
// and bounded call metadata from Elixir source text. PreScan returns the
// deterministic names used by the parent parser's repository import-map pass.
// The implementation stays parent-independent so Elixir-specific helpers do
// not add more surface area to the central parser dispatcher.
package elixir
