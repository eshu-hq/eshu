// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package relationship owns the Azure cloud-resource relationship edge
// writer (AzureCloudResourceEdgeWriter), which materializes Azure resource
// hierarchy evidence as edges between CloudResource nodes.
//
// The parent cypher package owns the shared Executor, Statement, Operation,
// and retry contracts; this leaf imports them as sourcecypher and the parent
// never imports this leaf. Reducer wiring in go/cmd/reducer imports this
// leaf directly for construction.
package relationship
