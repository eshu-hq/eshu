// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package queryplan validates fixture-backed query-plan expectations for hot
// Eshu graph read paths.
//
// The package is intentionally static: it does not connect to Neo4j,
// NornicDB, Postgres, or providers. Callers pass the graph schema statements
// they want to check against, and the validator enforces anchored Cypher
// shapes, bounded traversals, declared LIMIT/ORDER BY expectations, schema
// evidence names, optional backend plan-operator fixtures, and an exhaustive
// file/symbol/call-count inventory of production graph query execution sites.
// Every discovered callsite must link to registered hot entries or carry an
// explicit non-hot disposition. Handler entries also bind anchor fragments to
// their owning Go symbols. Live PROFILE assertions remain in the query package
// so this package keeps its no-network invariant.
package queryplan
