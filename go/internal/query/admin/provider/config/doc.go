// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package config holds the provider-config admin surface (Issue #6060, lane
// B): the ReadHandler endpoints (list, get, revisions) and the
// MutationHandler endpoints (create, update, revert, enable, disable, test
// connection) for external identity providers, with their detail models,
// write builders, login-readiness check, and store ports. Every route
// requires all-scope admin authentication. Shared audit glue lives in the
// admin/audit sibling package.
package config
