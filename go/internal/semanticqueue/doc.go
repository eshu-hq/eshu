// Package semanticqueue plans metadata-only semantic extraction queue records.
//
// The package is pure: it computes stable fingerprints, job identifiers, skip
// states, stale markers, and retry/dead-letter transitions without calling
// providers, opening databases, or retaining raw prompts and responses.
package semanticqueue
