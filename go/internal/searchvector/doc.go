// Package searchvector builds persisted local vector rows for curated Eshu
// search documents.
//
// The package reads active search-document rows through a caller-supplied
// document store, embeds the shared searchhybrid document text with a
// deterministic no-network Embedder, and writes derived vector metadata and
// values through caller-supplied stores. It performs no graph writes, no hosted
// provider calls, no API/MCP routing, and no queue scheduling.
package searchvector
