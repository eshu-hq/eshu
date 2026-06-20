// Package searchembedprovider provides governed hosted embedding adapters for
// curated Eshu search documents.
//
// The package turns an approved semantic provider profile into the
// searchhybrid.Embedder port used by search-vector builds and query-time vector
// lookup. It uses plain JSON HTTP, requires the search_documents source class
// and source-policy gate, and returns bounded errors that never include provider
// response bodies, raw source text, or credential material.
package searchembedprovider
