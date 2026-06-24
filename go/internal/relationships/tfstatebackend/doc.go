// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package tfstatebackend joins Terraform state snapshots to the config repo
// commit that declared their backend.
//
// State and config facts live in separate scopes: state in state_snapshot
// scope, config in the owning repository snapshot scope. The drift reducer
// needs both sides on the same candidate. This package resolves the
// (backend_kind, locator_hash) key to the latest sealed config snapshot that
// emitted a matching terraform_backends parser fact via a narrow
// TerraformBackendQuery port.
//
// V1 single-owner policy: at most one config repo may claim a given
// (backend_kind, locator_hash). Ambiguous ownership returns a typed error and
// the drift handler rejects the candidate with structural_mismatch.
package tfstatebackend
