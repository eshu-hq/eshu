// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package statestore reads Terraform-state admin evidence for the operator
// status surface: the most recent observed serial per state_snapshot scope
// (keyed by safe locator hash), and up to
// statuspkg.MaxTerraformStateRecentWarnings recent warning_fact rows per
// locator or Git backend-source handle.
//
// ReadTerraformStateAdminEvidence runs both bounded queries in one call and
// returns a TerraformStateAdminEvidence. It stays exported because the
// status family (still in the postgres root until its own #6693 leaf) reads
// through it to populate statuspkg.RawSnapshot.TerraformStateLastSerials and
// RawSnapshot.TerraformStateRecentWarnings. Everything else in this package
// is family-private. This package must not import the parent postgres
// package.
package statestore
