// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package advisory implements the supply-chain advisory query read models:
// the browsable CVE-intelligence catalog (AdvisoryCatalogStore, one bounded
// page of canonical advisories with no anchor required) and the source-only
// vulnerability advisory evidence detail (AdvisoryEvidenceStore, canonical
// advisory identities with per-source severity, affected-package/product,
// EPSS, KEV, and reference evidence grouped by BuildAdvisoryEvidenceRows),
// both served from active vulnerability source facts in Postgres.
//
// It moved out of root package query (#6060 lane A) so this family can be
// read, tested, and changed without pulling in the rest of the query surface.
// It depends only on dependency-neutral leaves -- querycontract (row-value
// decoders), querydecode (classified fact-decode failures), sdk/go/factschema
// (the typed vulnerability decode seams), and pgarray -- never on root
// package query itself, which would create an import cycle: root's
// supply_chain_advisory_alias.go imports this package for the compatibility
// aliases cmd/api and cmd/mcp-server still use.
//
// Two contracts callers must hold. A store reading through
// AdvisoryEvidenceQueryer must return rows for the exact
// ListAdvisoryEvidenceQuery parameter order; the evidence filter must carry
// an anchor (HasScope), because an unscoped read over the whole
// vulnerability fact corpus is rejected before any SQL runs. A caller
// reading AdvisoryEvidenceRow must treat a dropped Sources entry as a
// dead-lettered malformed fact (input_invalid), not as missing data: facts
// that fail typed-decode validation never contribute zero-valued rows.
//
// Capability registration stays in root package query
// (contract_supply_chain.go), which owns the router and always links into
// the production binary. The handler files stay in root until the hub PR3,
// so the advisory tests stay there too and reach this package as
// advisory.X; see README.md for which tests live where and why.
package advisory
