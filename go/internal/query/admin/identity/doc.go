// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package identity holds the tenant identity admin surface (Issue #6060,
// lane B): the ReadHandler endpoints (invitations, role assignments,
// roles, IdP providers, IdP group mappings, API tokens, audit reads) and the
// MutationHandler endpoints (invitation revokes, role assignment
// grants/revokes, IdP group mapping writes), with their row models and store
// ports. Every route requires all-scope admin authentication and reads or
// writes strictly within the caller's own tenant. Shared audit glue lives in
// the sibling audit package.
package identity
