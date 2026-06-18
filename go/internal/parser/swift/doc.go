// Package swift owns Swift parser extraction without depending on the parent
// parser dispatcher.
//
// Parse emits Swift imports, nominal types, functions, variables,
// parser-backed dead-code root kinds, and bounded call metadata from source
// text. Tree-sitter supplies declaration spans and ownership for multiline
// Swift syntax, while bounded helpers preserve existing attribute, variable,
// call, and root-kind behavior. PreScan returns deterministic names for the
// parent parser's repository import-map pass. The implementation stays
// parent-independent so Swift-specific heuristics can change without widening
// the central parser dispatcher.
package swift
