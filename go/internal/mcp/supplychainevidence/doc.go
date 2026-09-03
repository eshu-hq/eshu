// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package supplychainevidencetools defines pure route selection for the MCP
// supply-chain evidence family.
//
// Route decides which of the five supply-chain evidence tools this package
// owns and maps decoded arguments to a dependency-neutral internal request
// without executing it. The parent mcp package owns tool registration and its
// order, global route fanout, the private adapter, HTTP dispatch,
// authorization, timeouts, response budgets, envelopes, summaries, and
// telemetry. The query package owns the bounded, source-only reads behind
// these paths. This package runs no query and must keep the tool names,
// request paths, and query keys stable.
//
// The family is deliberately asymmetric. list_advisory_evidence and
// list_sbom_attestation_attachments page and default limit to 50;
// get_sbom_attestation_attachment_inventory also pages but defaults limit to
// 100, defaults offset to 0, and substitutes "attachment_status" for an
// absent or empty group_by. count_sbom_attestation_attachments carries the
// same SBOM filter keys as its listing and inventory siblings but never a
// limit, offset, or group_by: it aggregates a whole scope, so there is no
// page to size and nothing to seek past.
// get_vulnerability_scanner_read_contract forwards a single route selector
// naming the documented contract section the caller wants; the HTTP handler
// validates it.
//
// This family answers a narrower, evidence-listing surface than the
// supplychainimpact child: that package owns finding/count/inventory/explain
// selection over reducer-derived supply-chain-impact findings, while this one
// owns source-only advisory evidence, the scanner read contract, and SBOM
// attestation attachment evidence. The two packages are siblings, not layers,
// and neither imports the other.
package supplychainevidencetools
