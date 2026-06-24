// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package capabilitycatalog

import (
	"fmt"
	"sort"
	"strings"
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
	// FindingMissingAuthorizationGrant is a capability with no matching
	// authorization permission family.
	FindingMissingAuthorizationGrant FindingKind = "missing_authorization_grant"
	// FindingInvalidAuthorizationReference is an authorization catalog entry that
	// references an unknown role or data class.
	FindingInvalidAuthorizationReference FindingKind = "invalid_authorization_reference"
	// FindingStaleAuthorizationFamily is an authorization family whose prefixes
	// match no matrix capability.
	FindingStaleAuthorizationFamily FindingKind = "stale_authorization_family"
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
	authorization AuthorizationCatalog,
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
	findings = append(findings, authorizationFindings(matrix, authorization)...)

	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Kind != findings[j].Kind {
			return findings[i].Kind < findings[j].Kind
		}
		return findings[i].Subject < findings[j].Subject
	})
	return findings
}

func authorizationFindings(matrix Matrix, authorization AuthorizationCatalog) []Finding {
	index := newAuthorizationIndex(authorization)
	if !index.enabled {
		return nil
	}

	var findings []Finding
	matchedFamilies := map[string]bool{}
	for _, capability := range matrix.Capabilities {
		authz, ok := index.authorizationFor(capability.Capability)
		if !ok {
			findings = append(findings, Finding{
				Kind:    FindingMissingAuthorizationGrant,
				Subject: capability.Capability,
				Detail:  fmt.Sprintf("capability %q has no matching authorization permission family", capability.Capability),
			})
			continue
		}
		matchedFamilies[authz.Family] = true
	}

	for _, family := range authorization.PermissionFamilies {
		if strings.TrimSpace(family.Action) == "" {
			findings = append(findings, invalidAuthorizationReference(family.Family, "permission family is missing action"))
		}
		if len(family.CapabilityPrefixes) == 0 && !family.Planned {
			findings = append(findings, invalidAuthorizationReference(family.Family, "permission family is missing capability prefixes"))
		}
		for _, role := range family.DefaultRoles {
			defaultRole, ok := index.roles[role]
			if !ok {
				findings = append(findings, invalidAuthorizationReference(
					family.Family,
					fmt.Sprintf("default role %q is not declared", role),
				))
				continue
			}
			if !roleCoversPermissionFamily(defaultRole, family) {
				findings = append(findings, invalidAuthorizationReference(
					family.Family,
					fmt.Sprintf("default role %q does not grant action %q with the family data classes and scope levels", role, family.Action),
				))
			}
		}
		for _, dataClass := range family.DataClasses {
			if _, ok := index.dataClassSensitivity[dataClass]; !ok {
				findings = append(findings, invalidAuthorizationReference(
					family.Family,
					fmt.Sprintf("data class %q is not declared", dataClass),
				))
			}
		}
		if !matchedFamilies[family.Family] && !family.Planned {
			findings = append(findings, Finding{
				Kind:    FindingStaleAuthorizationFamily,
				Subject: family.Family,
				Detail:  fmt.Sprintf("authorization family %q matches no capability", family.Family),
			})
		}
	}

	for _, role := range authorization.Roles {
		if strings.TrimSpace(role.Role) == "" {
			findings = append(findings, invalidAuthorizationReference("<role>", "role id is required"))
		}
		for _, grant := range role.Grants {
			if strings.TrimSpace(grant.Action) == "" {
				findings = append(findings, invalidAuthorizationReference(role.Role, "role grant is missing action"))
			}
			for _, dataClass := range grant.DataClasses {
				if _, ok := index.dataClassSensitivity[dataClass]; !ok {
					findings = append(findings, invalidAuthorizationReference(
						role.Role,
						fmt.Sprintf("role grant data class %q is not declared", dataClass),
					))
				}
			}
		}
	}

	if authorization.BootstrapOwner.Role != "" {
		if _, ok := index.roles[authorization.BootstrapOwner.Role]; !ok {
			findings = append(findings, invalidAuthorizationReference(
				"bootstrap_owner",
				fmt.Sprintf("bootstrap owner role %q is not declared", authorization.BootstrapOwner.Role),
			))
		}
	}
	for _, role := range authorization.BootstrapOwner.DelegableRoles {
		if _, ok := index.roles[role]; !ok {
			findings = append(findings, invalidAuthorizationReference(
				"bootstrap_owner",
				fmt.Sprintf("delegable role %q is not declared", role),
			))
		}
	}

	return findings
}

func invalidAuthorizationReference(subject, detail string) Finding {
	return Finding{
		Kind:    FindingInvalidAuthorizationReference,
		Subject: subject,
		Detail:  detail,
	}
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
					capability.Capability, tool,
				),
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
