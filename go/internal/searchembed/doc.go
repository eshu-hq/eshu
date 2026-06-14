// Package searchembed provides local embedding implementations for Eshu search.
//
// The package owns deterministic, no-network embedders that satisfy the
// searchhybrid Embedder port. Its outputs are derived retrieval features only:
// callers may use them for semantic or hybrid ranking, but they must not promote
// vector similarity to canonical graph truth.
package searchembed
