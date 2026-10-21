// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package kubernetes owns the canonical Kubernetes node writers:
// NamespaceNodeWriter applies KubernetesNamespace node upserts and retracts,
// and WorkloadNodeWriter applies KubernetesWorkload node upserts, both
// through a sourcecypher.Executor with batching, nil-fencing, and
// fail-closed sequential retract dispatch.
//
// Both writers are constructed directly by cmd/reducer rather than through
// the CanonicalNodeWriter phase dispatch. Every Cypher statement template
// lives beside the writer that emits it and stays exported where the
// edge/materialized sibling reads it through. Templates move byte-identically
// with the writers; statement or predicate changes need their own issue with
// EXPLAIN and contention proof per cypher-query-rigor.
//
// The parent cypher package owns the shared contracts this leaf resolves
// through the sourcecypher import: Executor, GroupExecutor, Statement,
// the Operation and StatementMetadata keys, DefaultBatchSize,
// BuildBatchedStatements, and WrapRetryableNeo4jError. The parent never
// imports this leaf.
package kubernetes
