// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package facts

import "slices"

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
