// Package searchvector builds persisted vector rows for curated Eshu
// search documents.
//
// The package reads active search-document rows through a caller-supplied
// document store, embeds the shared searchhybrid document text with a
// caller-supplied Embedder, and writes derived vector metadata and values
// through caller-supplied stores. It performs no graph writes, no provider
// profile loading, no API/MCP routing, and no queue scheduling.
package searchvector
