// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package graph defines the source-local graph write contract and the Cypher
// builders used by writers and schema bootstrap.
//
// Writer is the narrow per-scope-generation write interface; Materialization
// and Record are its inputs. The package also owns Cypher statement and
// executor types kept here to avoid an import cycle with storage/cypher,
// canonical entity merge builders, batched UNWIND helpers, and the
// EnsureSchema constraint and index contract for the Neo4j and NornicDB
// dialects. Schema setup owns the
// SourceLocalRecord identity constraint required for source-local MERGE
// performance, keeps parser-matured infrastructure labels indexed before
// canonical writers upsert them, adds digest/tag-ref lookup support for OCI
// registry projection labels, adds uid lookup support for reducer-owned
// IncidentRoutingEvidence nodes, adds a NornicDB-only Function legacy-id lookup
// for bounded relationship-story fallback, and keeps backend-specific constraint
// translation inside the schema dialect and label-naming helpers. NornicDB
// drops direct composite uniqueness syntax, so canonical writers rely on
// projector-derived uid identity for those labels while Neo4j keeps the direct
// composite constraint. SchemaApplicationForBackend exposes the fingerprint and
// explicit compatibility list that graph-writing runtimes check before startup.
// Schema setup emits bounded progress logs for every DDL statement and treats context
// deadline or cancellation as a fail-fast signal. Generic DDL warnings remain
// non-fatal for permissive callers, while the strict schema helper returns an
// error after any non-context statement failure so deployment bootstrap does not
// mark a partial graph schema as applied.
//
// The schema is also the source of the write-side key-size guard (#7058):
// SchemaIndexKeys derives every label's indexed property keys from the same
// DDL, and GuardIndexKeyWrites uses them to drop any statement row that would
// put more than MaxIndexKeyBytes into one index key, so a single oversized
// source value skips one node instead of failing the atomic write it rides
// in. A new index is covered without touching any writer. The guard reads the
// write shapes Eshu writers emit; UnanalyzedIndexWrites reports any write to an
// indexed label in a shape it cannot read, so an unrecognized shape is loud
// rather than silently unguarded.
package graph
