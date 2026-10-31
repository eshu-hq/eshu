// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func supplyChainImpactFactID(write SupplyChainImpactWrite, finding SupplyChainImpactFinding) string {
	return supplyChainImpactFactKind + ":" + facts.StableID(
		supplyChainImpactFactKind,
		supplyChainImpactFactRowIdentity(write, finding),
	)
}

func supplyChainImpactStableFactKey(_ SupplyChainImpactWrite, finding SupplyChainImpactFinding) string {
	identity := supplyChainImpactLogicalIdentity(finding)
	parts := []string{
		"supply_chain_impact",
		strings.TrimSpace(fmt.Sprint(identity["cve_id"])),
		strings.TrimSpace(fmt.Sprint(identity["advisory_id"])),
		strings.TrimSpace(fmt.Sprint(identity["package_id"])),
		strings.TrimSpace(fmt.Sprint(identity["purl"])),
		strings.TrimSpace(fmt.Sprint(identity["product_criteria"])),
		strings.TrimSpace(fmt.Sprint(identity["match_criteria_id"])),
		strings.TrimSpace(fmt.Sprint(identity["observed_version"])),
		strings.TrimSpace(fmt.Sprint(identity["requested_range"])),
		strings.TrimSpace(fmt.Sprint(identity["impact_status"])),
		strings.TrimSpace(fmt.Sprint(identity["repository_id"])),
		strings.TrimSpace(fmt.Sprint(identity["subject_digest"])),
	}
	if finding.ScanTargetID != "" {
		// Stable keys can reach generic fact comparison surfaces. Preserve a
		// distinct, deterministic target key without exposing the protected
		// scan scope identifier outside its association table.
		parts = append(parts, facts.StableID("supply_chain_impact_target", map[string]any{
			"scan_target_id": strings.TrimSpace(finding.ScanTargetID),
		}))
	}
	return strings.Join(parts, ":")
}

func supplyChainImpactFactRowIdentity(write SupplyChainImpactWrite, finding SupplyChainImpactFinding) map[string]any {
	identity := supplyChainImpactLogicalIdentity(finding)
	identity["scope_id"] = strings.TrimSpace(write.ScopeID)
	identity["generation_id"] = strings.TrimSpace(write.GenerationID)
	// A derived row belongs to one admitted impact publication revision. Keep
	// the logical finding identity stable across passes while retaining the old
	// row and its protected authority association for audit and revocation.
	if finding.ScanTargetID != "" {
		identity["impact_fencing_token"] = write.FencingToken
	}
	return identity
}

func supplyChainImpactFindingID(finding SupplyChainImpactFinding) string {
	return supplyChainImpactFactKind + ":" + facts.StableID(
		supplyChainImpactFactKind,
		supplyChainImpactLogicalIdentity(finding),
	)
}

func supplyChainImpactLogicalIdentity(finding SupplyChainImpactFinding) map[string]any {
	identity := map[string]any{
		"cve_id":            strings.TrimSpace(finding.CVEID),
		"advisory_id":       strings.TrimSpace(finding.AdvisoryID),
		"package_id":        strings.TrimSpace(finding.PackageID),
		"purl":              strings.TrimSpace(finding.PURL),
		"product_criteria":  strings.TrimSpace(finding.ProductCriteria),
		"match_criteria_id": strings.TrimSpace(finding.MatchCriteriaID),
		"observed_version":  strings.TrimSpace(finding.ObservedVersion),
		"requested_range":   strings.TrimSpace(finding.RequestedRange),
		"impact_status":     strings.TrimSpace(string(finding.Status)),
		"subject_digest":    strings.TrimSpace(finding.SubjectDigest),
		"repository_id":     strings.TrimSpace(finding.RepositoryID),
	}
	if finding.ScanTargetID != "" {
		identity["scan_target_id"] = strings.TrimSpace(finding.ScanTargetID)
	}
	return identity
}
