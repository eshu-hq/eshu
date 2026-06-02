// Package searchbench defines the curated search benchmark evidence contract.
//
// The package keeps benchmark results tied to explicit EshuSearchDocument
// inputs, versioned semantic retrieval query suites, decay-scoring eval gates,
// current Postgres content-search baselines, NornicDB backend identity, and
// issue #1264 accuracy and operability metrics. It performs no database, graph,
// or NornicDB I/O; live adapters must feed measured observations into this
// contract and preserve derived truth labels.
package searchbench
