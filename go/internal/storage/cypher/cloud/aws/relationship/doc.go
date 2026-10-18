// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package relationship owns the AWS workload-to-cloud relationship edge
// writer (WorkloadCloudRelationshipWriter), which materializes exact
// workload-anchored CloudResource evidence as WorkloadInstance USES
// CloudResource edges. It never creates endpoint nodes.
//
// The parent cypher package owns the shared Executor, Statement, Operation,
// and retry contracts; this leaf imports them as sourcecypher and the parent
// never imports this leaf. The edge/materialized registry reads this leaf's
// exported retract and identity constants directly. Reducer wiring in
// go/cmd/reducer imports this leaf directly for construction.
package relationship
