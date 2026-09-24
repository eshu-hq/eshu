// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package compare serves POST /api/v0/compare/environments, which compares one
// workload's materialized state across two environments.
//
// Handler requires workload_id, left and right; a missing one is a 400. The
// limit bounds each environment's cloud-resource list through the impact
// family's list bounds (default 50, max 200) and reports truncation. The
// workload is resolved by id from the canonical graph, then each environment's
// WorkloadInstance and its USES-linked CloudResource rows are read. A missing
// instance falls back to service evidence read through a service.EvidenceReader
// and is reported as "inferred", or as "unsupported" when no evidence exists.
//
// A scoped caller whose grant is empty, or whose grant does not cover the
// workload's repo_id, gets the same "not found" response a missing workload
// gets, so the endpoint does not reveal which workloads exist. A profile
// without the capability answers 501. Graph read failures go through
// querycontract.WriteGraphReadError.
//
// Capability and Support declare the route's capability row once:
// internal/query/contract registers Support() for production and main_test.go
// registers it for this package's own tests.
//
// The package moved out of the root query package for #6642. Root keeps
// CompareHandler in compare_alias.go for cmd/api and cmd/mcp-server until the
// #6642 alias sweep.
package compare
