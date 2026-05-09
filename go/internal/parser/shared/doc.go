// Package shared contains dependency-safe helper contracts for language-owned
// parser packages.
//
// The package exists so child parser packages can share payload helpers,
// tree-sitter node helpers, source reads, and parser options without importing
// the parent parser dispatcher. Its helpers are language-neutral and preserve
// the payload shape consumed by collector materialization.
package shared
