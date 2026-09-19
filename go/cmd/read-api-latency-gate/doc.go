// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Command read-api-latency-gate is the backend-backed read-API latency
// benchmark for issue #6797. It seeds a synthetic corpus (ingestion scopes,
// generations, and fact_work_items across every collector kind
// scope.AllCollectorKinds reports, IaC content_entity fact_records with a
// realistic jsonb payload mix, and infra-labeled graph nodes) into a live
// Postgres and NornicDB/Neo4j backend, sweeps every no-arg GET route the
// generated surface inventory reports against a running eshu-api (true
// nearest-rank p95 over a warmup-discarded sample), and fails when any
// route exceeds its budget, a 5xx response occurs, the exercised-route
// coverage floor is not met, or an explicitly-budgeted route drops out of
// coverage.
//
// It exists to catch a regression like #6793 (infra resource aggregate
// full-graph scans, or the Postgres jsonb-detoast cost in
// currentInventoryCTE) or #6794 (status/readiness routes paying an
// expensive activeFactWorkItemsCTE query per collector-scoped read) before
// it reaches production: those regressions are invisible to hermetic tests
// because their cost only appears at repo/scope scale against a real
// backend.
//
// scripts/verify-read-api-latency-gate.sh is the orchestrator: it brings up
// the Docker Compose Postgres+NornicDB stack (the same one
// scripts/verify-golden-corpus-gate.sh uses), builds eshu-api and this
// binary, starts eshu-api, runs this binary to seed and sweep, and tears the
// stack down.
package main
