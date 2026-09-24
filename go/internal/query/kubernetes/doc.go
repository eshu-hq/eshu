// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package kubernetes serves GET /api/v0/kubernetes/correlations, the bounded
// read of reducer-owned Kubernetes workload correlations, and owns the
// Postgres store the supply-chain runtime probe uses to find current
// Kubernetes workloads for an image digest.
//
// Handler requires a limit of 1-200 (a value outside that range is a 400,
// not a clamp) and at least one anchor: scope_id, cluster_id,
// workload_object_id, namespace, image_ref or source_digest. It reads active
// reducer_kubernetes_correlation facts through a WorkloadCorrelationStore;
// PostgresCorrelationStore is the production implementation, and pages with an
// after_correlation_id keyset cursor. A scoped caller's grant binds
// fact.scope_id, and an empty grant returns an empty page without a store
// read. A profile without the capability answers 501, and a handler with no
// store answers 503; neither returns an empty page.
//
// PostgresRuntimeWorkloadStore implements the supply-chain
// KubernetesWorkloadCurrentInventoryFilter port. BuildRuntimeWorkloadQuery
// returns the exact SQL it issues, so root's live performance test can
// EXPLAIN it.
//
// Capability and Support declare the route's capability row once:
// internal/query/contract registers Support() for production and
// main_test.go registers it for this package's own tests.
//
// The package moved out of the root query package for #6642. Root keeps
// KubernetesHandler, PostgresKubernetesRuntimeWorkloadStore and both store
// constructors in kubernetes_alias.go for cmd/api, cmd/mcp-server and the
// supply-chain port assertion until the #6642 alias sweep.
package kubernetes
