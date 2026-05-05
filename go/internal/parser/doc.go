// Package parser owns the native Go parser registry, language adapters, and
// SCIP reduction support used to extract source-level entities and metadata.
//
// The package exposes a registry of language parsers, source-level entity
// and relationship extraction helpers, and SCIP support for index-derived
// facts. Parser changes must preserve fact truth: when a parser starts
// emitting a new entity, relationship, or metadata field, the relevant
// fixtures, fact contracts in internal/facts, and downstream docs must move
// in lockstep. Parsers must be deterministic given the same source bytes so
// retries and repair runs converge.
package parser
