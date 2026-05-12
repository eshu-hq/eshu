// Package swift owns Swift parser extraction without depending on the parent
// parser dispatcher.
//
// Parse emits Swift imports, nominal types, functions, variables,
// parser-backed dead-code root kinds, and bounded call metadata from source
// text, including declarations that use common Swift access and storage
// modifiers. PreScan returns deterministic names for the parent parser's
// repository import-map pass. The implementation stays parent-independent so
// Swift-specific heuristics can change without widening the central parser
// dispatcher.
package swift
