// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package resource owns the shared CloudResource graph writers: the
// CloudResource node writer (CloudResourceNodeWriter, including the
// ifadeterminismteeth build-tag split), the shared CloudResource edge
// writer (CloudResourceEdgeWriter), and the CloudResource retract dispatch
// the provider leaves anchor on.
//
// The parent cypher package owns the shared Executor, Statement, Operation,
// and retry contracts; this leaf imports them as sourcecypher and the parent
// never imports this leaf. Reducer wiring in go/cmd/reducer imports this
// leaf directly for construction.
package resource
