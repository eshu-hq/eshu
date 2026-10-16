// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package search owns the derived infra_search_key property that lets
// the infra resource search (#7245) narrow candidates through one Neo4j
// keyword FULLTEXT index (infra_search_key_ft) instead of scanning every infra
// label.
//
// The key joins the twelve properties deployment.InfraResourceFreeTextPredicate
// matches, computed in Cypher from the node's own properties by Clause. Every
// writer of those properties on a graph.InfraSearchKeyLabels label appends it
// through Statement, so the key cannot disagree with the fields. A joined key
// longer than graph.InfraSearchKeyMaxChars code points is not stored; the node
// gets infra_search_ovf = 'T' instead ('F' when the key is stored). The cap is
// load-bearing: the keyword analyzer makes the key one Lucene term, and a term
// past 32,766 bytes fails the commit and leaves the index FAILED.
//
// Dialect decides at statement construction whether the clause is appended:
// On for Neo4j, Off for NornicDB, which cannot evaluate the clause. Off returns
// the statement byte for byte. DialectForBackend maps the configured graph
// backend to the dialect; its zero value is invalid.
//
// Backfill keys existing nodes label by label in batched transactions and
// repairs stale keys; CountUnkeyed reports nodes no keying writer has touched;
// IndexState reads the index state for the read cutover's readiness probe;
// OversizedKeyCount is the pre-index readiness probe. All are Neo4j only.
package search
