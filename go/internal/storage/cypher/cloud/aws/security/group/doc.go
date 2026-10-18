// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package group owns the AWS security-group graph writers: the endpoint
// node writers (CIDR blocks, prefix lists, endpoints) and the reachability
// edge writer (SecurityGroupReachabilityWriter) with its projected-source
// ledger for UID-anchored retracts.
//
// The parent cypher package owns the shared Executor, Statement, Operation,
// and retry contracts; this leaf imports them as sourcecypher and the parent
// never imports this leaf. The edge/materialized registry reads this leaf's
// exported retract and identity constants directly. Reducer wiring in
// go/cmd/reducer imports this leaf directly for construction.
package group
