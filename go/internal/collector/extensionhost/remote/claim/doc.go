// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package claim is the prototype remote claimed-service client seam for
// the extension-host extraction (eshu-hq/eshu#8877).
//
// It speaks the merged workflow.v1 protocol table from eshu-hq/eshu-workflow:
// claim via ClaimNext, commit via ValidateAndPinClaimForCommit plus the
// claim-lifecycle operations, and live producer-grant re-read via a
// GrantReader. No core implementation handle crosses this boundary: no
// Postgres, graph, queue, lease, reducer, API, MCP, or workflow-store
// handle.
//
// The eshu-workflow module is pinned at its published v0.1.0 tag in
// go/go.mod. This package is additive only: in-tree production wiring
// (extensionhost Source, ClaimedService, buildClaimedService) is unchanged,
// and nothing in the production path imports this package yet. Wiring it into
// the extracted host binary is a cutover-harness gap, recorded in README.md.
package claim
