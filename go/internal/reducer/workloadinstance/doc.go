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
// nothing ran it again. [Check] asks [ExistenceLookup] (graph-backed:
// [GraphExistenceLookup], same MATCH shape as the writer) which anchors exist.
// [Wait] applies the shared commit-first decision (crossscope.DecideWait): the
// root handler commits every row, then returns [NotReadyError] while an anchor
// is missing, bounded by the (scope, domain) readiness-wait ledger's
// first-defer anchor. An unchanged poll looks up only the missing anchors.
//
// The package emits no metric itself. The root handler records
// eshu_dp_reducer_readiness_waits_total{domain,outcome}.
package workloadinstance
