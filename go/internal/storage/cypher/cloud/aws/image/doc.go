// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package image owns the AWS Lambda container-image edge writer
// (CloudResourceContainerImageEdgeWriter), which links CloudResource nodes
// to the container images their workloads run.
//
// The parent cypher package owns the shared Executor, Statement, Operation,
// and retry contracts; this leaf imports them as sourcecypher and the parent
// never imports this leaf. Reducer wiring in go/cmd/reducer imports this
// leaf directly for construction.
package image
