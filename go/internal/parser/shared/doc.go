// Package shared contains dependency-safe helper contracts for language-owned
// parser packages.
//
// The package exists so child parser packages can share payload helpers,
// tree-sitter node helpers, source reads, small value utilities, and parser
// options without importing the parent parser dispatcher. Its helpers are
// language-neutral and preserve the payload shape consumed by collector
// materialization. Go semantic-root options preserve the empty-method-list
// convention for imported package interface escapes without known method sets,
// explicit method lists for same-repository package contracts, and qualified
// method-call roots for imported package receiver types, while bucket sorting
// keeps the parent parser's line-number then name ordering contract.
package shared
