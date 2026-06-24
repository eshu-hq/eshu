// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package scopedtoken resolves hosted per-team bearer tokens into bounded
// authorization contexts for the Eshu API and MCP read surface.
//
// The hosted API/MCP surface authenticates with a single shared bearer token by
// default; every holder can read every indexed repository. This package adds an
// operator-managed registry that maps a presented token to a tenant, workspace,
// and the repository/ingestion-scope ids it may read, so a per-team token reads
// only that team's scope. Resolution is consumed by
// query.AuthMiddlewareWithScopedTokensAndGovernanceAudit, which enforces the
// scoped-route allowlist and bounded query filters.
//
// The registry stores only the SHA-256 hash of each token, never the token
// itself: the server hashes the presented credential and looks it up, so a
// leaked registry file cannot be replayed. Optional audit attribution fields
// must also be hash-shaped. Loading fails closed when an entry is malformed, a
// hash is duplicated, or a version is unsupported, and errors never include
// token-hash material, credentials, or other secret data.
//
// Operators issue and rotate tokens by editing the secret-mounted registry file
// referenced by ESHU_SCOPED_TOKENS_FILE: add an entry with the new token's hash
// and grants to issue, replace the hash to rotate, and remove the entry to
// revoke. When the variable is unset, scoped-token resolution is disabled and
// the surface keeps shared-token (or local dev-mode) behavior.
package scopedtoken
