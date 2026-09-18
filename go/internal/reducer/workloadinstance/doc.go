// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package workloadinstance is the WorkloadInstance readiness gate for the
// workload-cloud USES projection (#6785).
//
// The USES writer binds its source endpoint with a MATCH on
// (Workload {id})<-[:INSTANCE_OF]-(WorkloadInstance {environment}). Those
// instances are materialized from repository scopes, which run with no
// ordering against the AWS scope that carries the anchored resource. Before
// this gate the handler succeeded as a MATCH no-op when it ran first, and
// nothing ran it again. [Evaluate] asks [ExistenceLookup] (graph-backed:
// [GraphExistenceLookup], same MATCH shape as the writer) which anchors exist
// and returns a [Decision]. The root handler defers with [NotReadyError] while
// an anchor is missing, bounded by [MaxWait] of elapsed repair-cycle time, and
// past the bound commits with the missing anchors counted and logged.
//
// The package emits no metric itself. The root handler records
// eshu_dp_reducer_readiness_waits_total{domain,outcome}.
package workloadinstance
