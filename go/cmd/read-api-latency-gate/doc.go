// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Command read-api-latency-gate is the backend-backed read-API latency
// benchmark for issue #6797. It seeds a synthetic corpus (ingestion scopes,
// generations, and fact_work_items across every collector kind
// scope.AllCollectorKinds reports, IaC content_entity fact_records with a
// realistic jsonb payload mix, infra-labeled graph nodes, and uid-bearing
// graph nodes correlated with those IaC facts so /iac/resources can hydrate
// its Postgres candidates from the graph) into a live Postgres and
// NornicDB/Neo4j backend, sweeps every no-arg GET route the
// generated surface inventory reports against a running eshu-api (true
// nearest-rank p95 over a warmup-discarded sample), and fails when any
// route exceeds its budget, a 5xx response occurs, the exercised-route
// coverage floor is not met, or an explicitly-budgeted route drops out of
// coverage. It also meters the Postgres work each route costs per request
// (pg_stat_statements calls, rows, and buffer blocks) and fails when any
// counter exceeds its budget: buffers, unlike latency, separate a plan-shape
// regression such as #6794's from a healthy build by an order of magnitude
// regardless of runner speed. After seeding it reads per-label graph node
// counts back and fails if any label is short.
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
//
// -runs (default 1) repeats the counted sweep per route: run 1 is always the
// cold pass, and runs 2..-runs are warm passes issued with no additional
// warmup between them. -latency-report writes the full per-route
// distribution (cold and warm samples, warm n/p50/p95/min/max/stddev, the
// per-run p95 spread, and an identity block identifying the corpus, run
// shape, and binary) as JSON, before budget evaluation runs, so a leg that
// goes on to breach its budget still yields a report.
// scripts/compare-backend-latency.sh reads two such reports (one per
// backend) and renders a markdown comparison table, refusing to compare two
// legs whose identity disagrees in a field that must match (issue #6965
// phase 6: proving a latency claim across NornicDB and Neo4j needs
// distributions over repeated runs, not the single-sample comparisons this
// binary produced before).
package main
