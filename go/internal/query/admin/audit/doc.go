// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package audit holds the audit and permission glue the admin-handler family
// shares (Issue #6060, lane B): the Appender port, the auth-to-audit-actor
// mapping, the safe correlation helpers, the permission-feature gate, the
// identity hashes, and the optional-time row shaping.
//
// Each helper cites the query-root source it was repointed from. The logic
// sources stay canonical there (auth, permission catalog, local identity);
// this package only re-sources them through the extracted queryauth and
// querycontract leaves so the admin packages never import the query root,
// which would cycle back through handler.go and admin_alias.go.
package audit
