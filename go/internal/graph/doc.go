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
// registry projection labels, adds uid lookup support for reducer-owned
// IncidentRoutingEvidence nodes, and keeps backend-specific constraint
// translation inside the schema dialect and label-naming helpers. Schema setup
// emits bounded progress logs for every DDL statement and treats context
// deadline or cancellation as a fail-fast signal. Generic DDL warnings remain
// non-fatal for permissive callers, while the strict schema helper returns an
// error after any non-context statement failure so deployment bootstrap does not
// mark a partial graph schema as applied.
package graph
