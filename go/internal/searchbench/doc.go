// Package searchbench defines the curated search benchmark evidence contract.
//
// The package keeps benchmark results tied to explicit EshuSearchDocument
// inputs, versioned semantic retrieval query suites, decay-scoring eval gates,
// reranking eval gates, protocol recommendation gates, link-prediction candidate
// evidence gates, current Postgres content-search baselines, NornicDB backend
// identity, issue #1264 accuracy and operability metrics, issue #417 hybrid
// retrieval proof, issue #420 diagnostic relationship candidates, and issue
// #1298 stopped evidence, and issue #421 rerank/protocol close-out evidence.
// It performs no database, graph, protocol, reranker, or NornicDB I/O; live
// adapters must feed measured observations into this contract and preserve
// derived or candidate truth labels.
package searchbench
