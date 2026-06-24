// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package main runs the eshu-bootstrap-data-plane binary, which applies the
// Eshu Postgres and graph-backend schema DDL and exits.
//
// When invoked with --version or -v, it prints the embedded application
// version and exits before opening stores. Otherwise the binary opens Postgres
// through the runtime config helpers, applies the
// fact-store, queue, content, and audit DDL via postgres.ApplyBootstrap, then
// opens the configured graph backend (Neo4j or NornicDB) and applies the
// schema bootstrap through graph.EnsureSchemaWithBackendStrict. When the
// Postgres graph-schema marker is missing for NornicDB, the binary first
// inspects SHOW CONSTRAINTS and SHOW INDEXES and adopts the existing schema
// if every expected object is already present. Graph DDL statements run under
// a per-statement deadline so startup failures name the stuck schema phase
// instead of waiting for the outer Kubernetes or Compose deadline. After every
// graph statement succeeds, Postgres records the backend/schema fingerprint so
// preserved-volume restarts can skip already-applied graph DDL. All DDL uses
// CREATE ... IF NOT EXISTS so the binary remains safe to run as a Kubernetes
// Job or Compose `db-migrate` service before the long-running runtimes start.
package main
