// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package incident

import "github.com/eshu-hq/eshu/go/internal/facts/contract"

// IncidentContextBuildIncidentRoutingEvidenceInputsBinding declares this package's verified reducer consumption of
// incident_context kinds (issue #6629). Build-time metadata only.
var IncidentContextBuildIncidentRoutingEvidenceInputsBinding = contract.Bind(contract.Binding{
	ID: "incident/incident_build_routing_evidence_inputs_context", FamilyID: "incident_context",
	ReducerDir: "go/internal/reducer/incident", Symbol: "buildIncidentRoutingEvidenceInputs",
	Kinds: []string{"incident.record"},
})

// IncidentRoutingBuildIncidentRoutingEvidenceInputsBinding declares this package's verified reducer consumption of
// incident_routing kinds (issue #6629). Build-time metadata only.
var IncidentRoutingBuildIncidentRoutingEvidenceInputsBinding = contract.Bind(contract.Binding{
	ID: "incident/incident_build_routing_evidence_inputs_routing", FamilyID: "incident_routing",
	ReducerDir: "go/internal/reducer/incident", Symbol: "buildIncidentRoutingEvidenceInputs",
	Kinds: []string{"incident_routing.applied_pagerduty_resource", "incident_routing.coverage_warning", "incident_routing.observed_pagerduty_service"},
})
