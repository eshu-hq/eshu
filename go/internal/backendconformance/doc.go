// Package backendconformance defines the graph-backend conformance matrix and
// reusable read/write corpora for Chunk 5 of the embedded local backends ADR.
//
// The package deliberately keeps the default test path deterministic and free
// of live database requirements. Adapter-specific integration tests can import
// the same corpora and run them against Neo4j, NornicDB, Compose, or remote
// proof environments.
package backendconformance
