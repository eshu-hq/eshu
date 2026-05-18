// Package graph defines the source-local graph write contract and the Cypher
// builders used by writers and schema bootstrap.
//
// Writer is the narrow per-scope-generation write interface; Materialization
// and Record are its inputs. The package also owns Cypher statement and
// executor types kept here to avoid an import cycle with storage/cypher,
// canonical entity merge builders, batched UNWIND helpers, the file and
// repository deletion mutations, and the EnsureSchema constraint and index
// contract for the Neo4j and NornicDB dialects. Schema setup owns the
// SourceLocalRecord identity constraint required for source-local MERGE
// performance, keeps parser-matured infrastructure labels indexed before
// canonical writers upsert them, adds digest/tag-ref lookup support for OCI
// registry projection labels, and keeps backend-specific constraint translation
// inside the schema dialect and label-naming helpers. Schema setup emits
// bounded progress logs for every DDL statement and treats context deadline or
// cancellation as a fail-fast signal while keeping generic DDL warnings
// non-fatal.
package graph
