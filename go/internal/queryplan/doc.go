// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package queryplan validates fixture-backed query-plan expectations for hot
// Eshu graph read paths.
//
// The package is intentionally static: it does not connect to Neo4j,
// NornicDB, Postgres, or providers. Callers pass the graph schema statements
// they want to check against, and the validator enforces anchored Cypher
// shapes, bounded traversals, declared LIMIT/ORDER BY expectations, schema
// evidence names, and optional backend plan-operator fixtures.
package queryplan
