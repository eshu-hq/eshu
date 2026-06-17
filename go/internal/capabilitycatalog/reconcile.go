package capabilitycatalog

import (
	"fmt"
	"sort"
)

// FindingKind classifies a reconciliation finding.
type FindingKind string

const (
	// FindingOrphanMCPTool is an MCP tool not referenced by any capability and
	// not recorded in the overlay tool exemptions.
	FindingOrphanMCPTool FindingKind = "orphan_mcp_tool"
	// FindingUnmatchedSurface is a capability-declared tool with no MCP-registry
	// match and no overlay non-MCP-surface declaration.
	FindingUnmatchedSurface FindingKind = "unmatched_surface"
	// FindingStaleOverlayCapability is an overlay capability entry whose id is
	// absent from the matrix.
	FindingStaleOverlayCapability FindingKind = "stale_overlay_capability"
	// FindingStaleToolExemption is a tool exemption whose tool is absent from
	// the MCP registry.
	FindingStaleToolExemption FindingKind = "stale_tool_exemption"
	// FindingStaleNonMCPSurface is a non-MCP-surface declaration whose tool is
	// not referenced by any capability.
	FindingStaleNonMCPSurface FindingKind = "stale_non_mcp_surface"
	// FindingMissingMaturityReason is a maturity override without a reason.
	FindingMissingMaturityReason FindingKind = "missing_maturity_reason"
	// FindingInvalidOverlayMaturity is an overlay maturity that is not an
	// overlay-only state.
	FindingInvalidOverlayMaturity FindingKind = "invalid_overlay_maturity"
)

// Finding is one reconciliation problem between the catalog overlay and live
// code signals. Findings are advisory output from Build; the generator's verify
// mode treats a non-empty list as a gate failure.
type Finding struct {
	// Kind classifies the finding.
	Kind FindingKind `json:"kind"`
	// Subject is the capability id or tool name the finding concerns.
	Subject string `json:"subject"`
	// Detail is a human-readable explanation.
	Detail string `json:"detail"`
}

// reconcile produces the findings that compare the overlay and live signals
// against the matrix. referencedTools is the set of tools declared by any
// capability; nonMCP is the overlay's declared non-MCP surfaces.
func reconcile(
	matrix Matrix,
	overlay Overlay,
	signals Signals,
	overlayByID map[string]OverlayCapability,
	referencedTools map[string]bool,
	nonMCP map[string]SurfaceKind,
) []Finding {
	matrixIDs := map[string]struct{}{}
	for _, capability := range matrix.Capabilities {
		matrixIDs[capability.Capability] = struct{}{}
	}
	exemptTools := map[string]struct{}{}
	for _, exemption := range overlay.ToolExemptions {
		exemptTools[exemption.Tool] = struct{}{}
	}

	var findings []Finding
	findings = append(findings, unmatchedSurfaceFindings(matrix, signals, nonMCP)...)
	findings = append(findings, orphanToolFindings(signals, referencedTools, exemptTools)...)
	findings = append(findings, overlayFindings(overlay, overlayByID, matrixIDs)...)
	findings = append(findings, exemptionAndSurfaceFindings(overlay, signals, referencedTools, matrixIDs)...)

	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Kind != findings[j].Kind {
			return findings[i].Kind < findings[j].Kind
		}
		return findings[i].Subject < findings[j].Subject
	})
	return findings
}

func unmatchedSurfaceFindings(matrix Matrix, signals Signals, nonMCP map[string]SurfaceKind) []Finding {
	var findings []Finding
	for _, capability := range matrix.Capabilities {
		for _, tool := range capability.Tools {
			if signals.MCPTools[tool] || nonMCP[tool] != "" {
				continue
			}
			findings = append(findings, Finding{
				Kind:    FindingUnmatchedSurface,
				Subject: tool,
				Detail: fmt.Sprintf(
					"capability %q declares tool %q with no MCP registry match and no non_mcp_surfaces declaration",
					capability.Capability, tool),
			})
		}
	}
	return findings
}

func orphanToolFindings(signals Signals, referencedTools map[string]bool, exemptTools map[string]struct{}) []Finding {
	var findings []Finding
	for tool := range signals.MCPTools {
		if referencedTools[tool] {
			continue
		}
		if _, ok := exemptTools[tool]; ok {
			continue
		}
		findings = append(findings, Finding{
			Kind:    FindingOrphanMCPTool,
			Subject: tool,
			Detail:  fmt.Sprintf("MCP tool %q is not mapped to a capability and is not exempt", tool),
		})
	}
	return findings
}

func overlayFindings(overlay Overlay, overlayByID map[string]OverlayCapability, matrixIDs map[string]struct{}) []Finding {
	var findings []Finding
	for _, oc := range overlay.Capabilities {
		if _, ok := matrixIDs[oc.Capability]; !ok {
			findings = append(findings, Finding{
				Kind:    FindingStaleOverlayCapability,
				Subject: oc.Capability,
				Detail:  fmt.Sprintf("overlay capability %q is absent from the matrix", oc.Capability),
			})
			continue
		}
		if oc.Maturity == "" {
			continue
		}
		// An invalid maturity is already wrong; do not also demand a reason for
		// it. Only a valid override needs a reason.
		if _, ok := overlayMaturities[oc.Maturity]; !ok {
			findings = append(findings, Finding{
				Kind:    FindingInvalidOverlayMaturity,
				Subject: oc.Capability,
				Detail:  fmt.Sprintf("overlay maturity %q is not an overlay-only state (gated, degraded)", oc.Maturity),
			})
			continue
		}
		if oc.Reason == "" {
			findings = append(findings, Finding{
				Kind:    FindingMissingMaturityReason,
				Subject: oc.Capability,
				Detail:  fmt.Sprintf("overlay maturity override for %q is missing a maturity_reason", oc.Capability),
			})
		}
	}
	return findings
}

func exemptionAndSurfaceFindings(overlay Overlay, signals Signals, referencedTools map[string]bool, matrixIDs map[string]struct{}) []Finding {
	var findings []Finding
	for _, exemption := range overlay.ToolExemptions {
		if !signals.MCPTools[exemption.Tool] {
			findings = append(findings, Finding{
				Kind:    FindingStaleToolExemption,
				Subject: exemption.Tool,
				Detail:  fmt.Sprintf("tool exemption %q is not present in the MCP registry", exemption.Tool),
			})
		}
		if exemption.Capability != "" {
			if _, ok := matrixIDs[exemption.Capability]; !ok {
				findings = append(findings, Finding{
					Kind:    FindingStaleToolExemption,
					Subject: exemption.Tool,
					Detail:  fmt.Sprintf("tool exemption %q maps to capability %q which is absent from the matrix", exemption.Tool, exemption.Capability),
				})
			}
		}
	}
	for _, surface := range overlay.NonMCPSurfaces {
		if !referencedTools[surface.Tool] {
			findings = append(findings, Finding{
				Kind:    FindingStaleNonMCPSurface,
				Subject: surface.Tool,
				Detail:  fmt.Sprintf("non_mcp_surfaces tool %q is not referenced by any capability", surface.Tool),
			})
		}
	}
	return findings
}
