// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package gcp builds reducer intents for GCP resource-node and relationship-edge
// materialization from one immutable fact lookup.
//
// Callers pass an immutable intent.FactLookup that root projector assembly
// constructs and owns for one generation. Root also owns dispatcher order,
// final sorting, queue writes, retries, and telemetry.
// Each builder emits at most one intent, uses the earliest fact of its exact
// kind, prefers source-ref identity over the collector fallback, and emits
// nothing when that kind is absent. Both builders use the same
// gcp_resource_materialization:<scope> readiness key so relationship work waits
// for the resource-node publication.
package gcp
