// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package drift serves POST /api/v0/terraform/config-state-drift/findings,
// the bounded read of reducer-materialized Terraform config-vs-state drift
// findings for one state-snapshot scope (issue #5442).
//
// Handler requires a scope_id with the "state_snapshot:" prefix; an optional
// outcome must be exact, derived, ambiguous or unresolved, and limit defaults
// to 100 and is capped at 500. A missing or malformed scope_id or outcome is a
// 400. It reads active reducer_terraform_config_state_drift_finding facts
// through a FindingStore; PostgresFindingStore, built by
// NewPostgresFindingStore, adapts postgres.TerraformConfigStateDriftFindingStore
// as the production implementation. A scope whose backend never resolves to a
// config repo is reported as one "unresolved" finding, not an empty page.
//
// A scoped caller must hold a grant on the exact scope_id, or it gets an empty
// page without a store read; the grant is also bound onto FindingFilter so the
// SQL layer enforces it a second time. Ambiguous-owner candidates and
// evidence scope_ids outside the caller's grant are withheld or redacted
// before the response is written. A profile without the capability answers
// 501, and a handler with no store answers 503; neither returns an empty page.
//
// Capability and Support declare the route's capability row once:
// internal/query/contract registers Support() for production and
// main_test.go registers it for this package's own tests.
//
// The package moved out of the root query package for #6642. Root keeps
// TerraformConfigStateDriftHandler and
// NewPostgresTerraformConfigStateDriftFindingStore in terraform_drift_alias.go
// for cmd/api and cmd/mcp-server until the #6642 alias sweep.
package drift
