// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package tenantstore persists tenant workspace grant truth: which workspace
// answers for a tenant, which scopes and repositories that workspace grants,
// and the policy revision each grant was issued under.
//
// TenantWorkspaceGrantStore upserts tenant, workspace, scope-grant, and
// repository-grant rows, lists active grants, and resolves the single primary
// workspace for a tenant, failing with ErrTenantWorkspaceAmbiguous when more
// than one active workspace exists and ErrTenantWorkspaceNotFound when none
// does. The DDL and statement text live in workspace_grants_schema.go and
// move byte-identically with the store.
//
// The UpsertTenantRecordQuery and UpsertWorkspaceRecordQuery statements stay
// exported because the identity bootstrap path (still in the postgres root
// until its own #6693 leaf) writes the bootstrap tenant and workspace in the
// same transaction as the initial local identity credential. Shared
// null/blank value shaping lives in the sibling db package, not here.
// This package must not import the parent postgres package.
package tenantstore
