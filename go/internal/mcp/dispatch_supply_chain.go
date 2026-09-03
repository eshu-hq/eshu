// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"github.com/eshu-hq/eshu/go/internal/mcp/routecontract"
	supplychainevidencetools "github.com/eshu-hq/eshu/go/internal/mcp/supplychainevidence"
)

// supplyChainEvidenceRoute adapts the child package's supply-chain evidence
// request into the root dispatcher's transport route. The family's five arms
// (the vulnerability-scanner read contract, the advisory-evidence listing,
// and the SBOM/attestation attachment listing, count, and inventory) lived in
// this file and dispatch_sbom_attachment_aggregates.go before the extraction;
// this file's name is reused as the adapter's home, and
// dispatch_sbom_attachment_aggregates.go is removed, rather than adding a new
// dispatch file, so the root non-test file count moves by exactly the one
// file this extraction actually removes.
func supplyChainEvidenceRoute(toolName string, args map[string]any) (*route, bool) {
	return adaptChildRoute(supplychainevidencetools.Route(toolName, routecontract.Arguments(args)))
}
