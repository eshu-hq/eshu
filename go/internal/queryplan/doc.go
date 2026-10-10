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
// explicit non-hot disposition; grandfathered prose dispositions are frozen to
// exact source digests. Handler registrations store no copied Cypher: the query
// package binds exact production-builder bytes and builder source by SHA-256
// before applying the static shape rules and live PROFILE assertions. Anchor
// fragments remain bound to their owning builder symbols, while this package
// keeps its no-network invariant. Global entity and code graph variants are
// fail-closed: only repository-anchored graph shapes remain in the live PROFILE
// family, while global name lookup is covered by its bounded Postgres SQL and
// plan proof.
//
// The versioned #7881 pilot contract extends entries in the existing manifest.
// It pins required emitted variant and parameter-case identities, structured
// authorization and page bounds, workload and numeric budgets, and reproducible
// evidence inputs. A scoped PostgreSQL call census is separate from the graph
// Run/RunSingle inventory and marks unscoped files as excluded. JSON evidence
// validation rejects missing or stale proof and requires independent oracle
// provenance, paired base/candidate results and timings, full plans and work
// metrics, and every required case. It makes no claim that static validation
// alone proves database semantics or plan quality.
package queryplan
