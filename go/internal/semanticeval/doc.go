// Package semanticeval scores Eshu semantic retrieval evaluation runs.
//
// The package is intentionally storage-free. It compares checked-in eval cases
// with ranked result handles from current exact Eshu paths or future
// NornicDB-backed semantic paths, then reports recall@K, precision@K, nDCG@K,
// false canonical claims, forbidden hits, unsupported cases, and latency.
package semanticeval
