// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package serviceintel

// sectionSpec is the static definition of one fixed report section: its title,
// the prompt family / tool / route it composes from, and the bounded next call
// to recommend when the section is empty or unsupported. Every tool, route, and
// playbook named here is a real, executable Eshu surface; the composer never
// invents an identifier.
type sectionSpec struct {
	Kind         SectionKind
	Title        string
	PromptFamily string
	Tool         string
	Route        string
	// Fallback is the bounded next call surfaced when the section lacks resolved
	// evidence, so an empty or unsupported section is always actionable.
	Fallback NextCall
}

// sectionCatalog is the closed, ordered list of report sections. The composer
// iterates it in order, so a report always carries the same sections in the
// same positions regardless of which inputs the caller supplied.
var sectionCatalog = []sectionSpec{
	{
		Kind:         SectionIdentity,
		Title:        "Service identity",
		PromptFamily: "service.story",
		Tool:         "get_service_story",
		Route:        "/api/v0/services/{service_name}/story",
		Fallback: NextCall{
			Tool:     "get_service_story",
			Route:    "/api/v0/services/{service_name}/story",
			Playbook: "service_story_citation",
			Reason:   "resolve the canonical service identity before reading the rest of the report",
		},
	},
	{
		Kind:         SectionCodeToRuntime,
		Title:        "Code-to-runtime trace",
		PromptFamily: "service.story",
		Tool:         "get_service_story",
		Route:        "/api/v0/services/{service_name}/story",
		Fallback: NextCall{
			Tool:     "get_service_story",
			Route:    "/api/v0/services/{service_name}/story",
			Playbook: "service_story_citation",
			Reason:   "trace entrypoints and network paths from source to runtime for this service",
		},
	},
	{
		Kind:         SectionDeploymentConfig,
		Title:        "Deployment and configuration influence",
		PromptFamily: "service.story",
		Tool:         "get_service_story",
		Route:        "/api/v0/services/{service_name}/story",
		Fallback: NextCall{
			Tool:     "compare_environments",
			Playbook: "service_story_citation",
			Reason:   "gather deployment lanes and per-environment configuration influence for this service",
		},
	},
	{
		Kind:         SectionSupplyChain,
		Title:        "Supply-chain evidence",
		PromptFamily: "supply-chain.impact",
		Tool:         "get_supply_chain_impact_inventory",
		Route:        "/api/v0/supply-chain/impact/inventory",
		Fallback: NextCall{
			Tool:     "get_supply_chain_impact_inventory",
			Route:    "/api/v0/supply-chain/impact/inventory",
			Playbook: "supply_chain_impact_explanation",
			Reason:   "inventory image, dependency, and build-provenance evidence for this service",
		},
	},
	{
		Kind:         SectionIncidentsSupport,
		Title:        "Incidents, support, and runbook evidence",
		PromptFamily: "incident.context",
		Tool:         "get_incident_context",
		Route:        "/api/v0/incidents/{incident_id}/context",
		Fallback: NextCall{
			Tool:     "get_incident_context",
			Route:    "/api/v0/incidents/{incident_id}/context",
			Playbook: "incident_context_evidence_path",
			Reason:   "pull incident, support, and runbook evidence routed to this service",
		},
	},
}

// specForKind returns the static spec for a section kind.
func specForKind(kind SectionKind) (sectionSpec, bool) {
	for _, spec := range sectionCatalog {
		if spec.Kind == kind {
			return spec, true
		}
	}
	return sectionSpec{}, false
}
