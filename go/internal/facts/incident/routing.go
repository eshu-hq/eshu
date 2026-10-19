// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package incident

import (
	"slices"

	"github.com/eshu-hq/eshu/go/internal/facts/contract"
)

const (
	// IncidentRoutingAppliedPagerDutyResourceFactKind identifies one PagerDuty
	// routing resource observed from applied Terraform state.
	IncidentRoutingAppliedPagerDutyResourceFactKind = "incident_routing.applied_pagerduty_resource"
	// IncidentRoutingAppliedAlertRouteFactKind identifies one alert route
	// resource observed from applied Terraform state.
	IncidentRoutingAppliedAlertRouteFactKind = "incident_routing.applied_alert_route"
	// IncidentRoutingObservedPagerDutyServiceFactKind identifies one live
	// PagerDuty service observed from the PagerDuty REST API.
	IncidentRoutingObservedPagerDutyServiceFactKind = "incident_routing.observed_pagerduty_service"
	// IncidentRoutingObservedPagerDutyIntegrationFactKind identifies one live
	// PagerDuty service integration observed from the PagerDuty REST API.
	IncidentRoutingObservedPagerDutyIntegrationFactKind = "incident_routing.observed_pagerduty_integration"
	// IncidentRoutingCoverageWarningFactKind identifies bounded coverage gaps
	// while collecting incident-routing evidence.
	IncidentRoutingCoverageWarningFactKind = "incident_routing.coverage_warning"

	// IncidentRoutingSchemaVersionV1 is the first incident-routing source fact
	// schema.
	IncidentRoutingSchemaVersionV1 = "1.0.0"
)

var incidentRoutingFactKinds = []string{
	IncidentRoutingAppliedPagerDutyResourceFactKind,
	IncidentRoutingAppliedAlertRouteFactKind,
	IncidentRoutingObservedPagerDutyServiceFactKind,
	IncidentRoutingObservedPagerDutyIntegrationFactKind,
	IncidentRoutingCoverageWarningFactKind,
}

var incidentRoutingSchemaVersions = map[string]string{
	IncidentRoutingAppliedPagerDutyResourceFactKind:     IncidentRoutingSchemaVersionV1,
	IncidentRoutingAppliedAlertRouteFactKind:            IncidentRoutingSchemaVersionV1,
	IncidentRoutingObservedPagerDutyServiceFactKind:     IncidentRoutingSchemaVersionV1,
	IncidentRoutingObservedPagerDutyIntegrationFactKind: IncidentRoutingSchemaVersionV1,
	IncidentRoutingCoverageWarningFactKind:              IncidentRoutingSchemaVersionV1,
}

// IncidentRoutingFactKinds returns accepted incident-routing source fact kinds
// in source-contract order.
func IncidentRoutingFactKinds() []string {
	return slices.Clone(incidentRoutingFactKinds)
}

// IncidentRoutingSchemaVersion returns the schema version for an
// incident-routing source fact kind.
func IncidentRoutingSchemaVersion(factKind string) (string, bool) {
	version, ok := incidentRoutingSchemaVersions[factKind]
	return version, ok
}

// IncidentRoutingFamilyDeclaration is the typed fact-family contract declaration for the
// incident_routing family (issue #6629). It is build-time metadata: the
// fact-kind-registry generator discovers it statically and derives
// the registry, shards, and catalog from it.
var IncidentRoutingFamilyDeclaration = contract.Declare(contract.Family{
	ID:                     "incident_routing",
	FactDir:                "go/internal/facts/incident",
	OwnerRepository:        "eshu-hq/eshu",
	LifecycleOwner:         "go/internal/facts",
	SchemaVersion:          "1.0.0",
	ReducerDomain:          "incident_routing_materialization",
	ProjectionHook:         "incident_routing_materialization",
	AdmissionHook:          "facts.ValidateSchemaVersion",
	ReadSurface:            "GET /api/v0/incidents/{incident_id}/context",
	TruthProfile:           "provider_gated",
	ProviderKeyIndependent: false,
	Kinds: []contract.Kind{
		{Kind: "incident_routing.applied_alert_route", PayloadSchema: "sdk/go/factschema/schema/incident_routing.applied_alert_route.v1.schema.json", Disposition: "unconsumed", NoReducerReason: "Round-2 verified no real consumer: searches for the kind constant and wire literal across reducer, projector, query, storage, and relationships trees returned no consumption.", NoReducerEvidence: "go/internal/mcp/kind_disclosure_ledger.go (kindDisclosureEntries, #5474 backfill)"},
		{Kind: "incident_routing.applied_pagerduty_resource", PayloadSchema: "sdk/go/factschema/schema/incident_routing.applied_pagerduty_resource.v1.schema.json"},
		{Kind: "incident_routing.coverage_warning", PayloadSchema: "sdk/go/factschema/schema/incident_routing.coverage_warning.v1.schema.json"},
		{Kind: "incident_routing.observed_pagerduty_integration", PayloadSchema: "sdk/go/factschema/schema/incident_routing.observed_pagerduty_integration.v1.schema.json", Disposition: "unconsumed", NoReducerReason: "Round-2 verified no real consumer: searches for the kind constant and wire literal across reducer, projector, query, storage, and relationships trees returned no consumption.", NoReducerEvidence: "go/internal/mcp/kind_disclosure_ledger.go (kindDisclosureEntries, #5474 backfill)"},
		{Kind: "incident_routing.observed_pagerduty_service", PayloadSchema: "sdk/go/factschema/schema/incident_routing.observed_pagerduty_service.v1.schema.json"},
	},
	Producers: []contract.Producer{
		{ID: "collector-pagerduty", Repository: "eshu-hq/eshu", Path: "go/internal/collector/pagerduty", PlannedRepository: "eshu-hq/eshu-collector-pagerduty", AuthorityMode: "core_issued_grant", Kinds: []string{"incident_routing.observed_pagerduty_integration", "incident_routing.observed_pagerduty_service"}},
		{ID: "collector-terraform-state", Repository: "eshu-hq/eshu", Path: "go/internal/collector/terraformstate", PlannedRepository: "eshu-hq/eshu-collector-terraform-state", AuthorityMode: "core_issued_grant", Kinds: []string{"incident_routing.applied_alert_route", "incident_routing.applied_pagerduty_resource", "incident_routing.coverage_warning"}},
	},
})
