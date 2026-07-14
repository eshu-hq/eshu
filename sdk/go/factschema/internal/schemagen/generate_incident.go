// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package schemagen

import (
	incidentv1 "github.com/eshu-hq/eshu/sdk/go/factschema/incident/v1"
)

// IncidentRecordSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "incident.record" payload.
const IncidentRecordSchemaID = schemaBaseID + "incident/v1/record.schema.json"

// IncidentRecordSchema returns the JSON Schema bytes for
// incidentv1.IncidentRecord.
func IncidentRecordSchema() ([]byte, error) {
	return reflectSchema(IncidentRecordSchemaID, "Eshu incident.record Payload (schema version 1)", &incidentv1.IncidentRecord{})
}

// IncidentLifecycleEventSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "incident.lifecycle_event" payload.
const IncidentLifecycleEventSchemaID = schemaBaseID + "incident/v1/lifecycle_event.schema.json"

// IncidentLifecycleEventSchema returns the JSON Schema bytes for
// incidentv1.LifecycleEvent.
func IncidentLifecycleEventSchema() ([]byte, error) {
	return reflectSchema(IncidentLifecycleEventSchemaID, "Eshu incident.lifecycle_event Payload (schema version 1)", &incidentv1.LifecycleEvent{})
}

// ChangeRecordSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "change.record" payload.
const ChangeRecordSchemaID = schemaBaseID + "incident/v1/change_record.schema.json"

// ChangeRecordSchema returns the JSON Schema bytes for incidentv1.ChangeRecord.
func ChangeRecordSchema() ([]byte, error) {
	return reflectSchema(ChangeRecordSchemaID, "Eshu change.record Payload (schema version 1)", &incidentv1.ChangeRecord{})
}

// IncidentRoutingAppliedPagerDutyResourceSchemaID is the checked-in JSON Schema
// $id for the schema-version-1 "incident_routing.applied_pagerduty_resource"
// payload.
const IncidentRoutingAppliedPagerDutyResourceSchemaID = schemaBaseID + "incident/v1/applied_pagerduty_resource.schema.json"

// IncidentRoutingAppliedPagerDutyResourceSchema returns the JSON Schema bytes
// for incidentv1.AppliedPagerDutyResource.
func IncidentRoutingAppliedPagerDutyResourceSchema() ([]byte, error) {
	return reflectSchema(IncidentRoutingAppliedPagerDutyResourceSchemaID, "Eshu incident_routing.applied_pagerduty_resource Payload (schema version 1)", &incidentv1.AppliedPagerDutyResource{})
}

// IncidentRoutingAppliedAlertRouteSchemaID is the checked-in JSON Schema $id for
// the schema-version-1 "incident_routing.applied_alert_route" payload.
const IncidentRoutingAppliedAlertRouteSchemaID = schemaBaseID + "incident/v1/applied_alert_route.schema.json"

// IncidentRoutingAppliedAlertRouteSchema returns the JSON Schema bytes for
// incidentv1.AppliedAlertRoute.
func IncidentRoutingAppliedAlertRouteSchema() ([]byte, error) {
	return reflectSchema(IncidentRoutingAppliedAlertRouteSchemaID, "Eshu incident_routing.applied_alert_route Payload (schema version 1)", &incidentv1.AppliedAlertRoute{})
}

// IncidentRoutingObservedPagerDutyServiceSchemaID is the checked-in JSON Schema
// $id for the schema-version-1 "incident_routing.observed_pagerduty_service"
// payload.
const IncidentRoutingObservedPagerDutyServiceSchemaID = schemaBaseID + "incident/v1/observed_pagerduty_service.schema.json"

// IncidentRoutingObservedPagerDutyServiceSchema returns the JSON Schema bytes
// for incidentv1.ObservedPagerDutyService.
func IncidentRoutingObservedPagerDutyServiceSchema() ([]byte, error) {
	return reflectSchema(IncidentRoutingObservedPagerDutyServiceSchemaID, "Eshu incident_routing.observed_pagerduty_service Payload (schema version 1)", &incidentv1.ObservedPagerDutyService{})
}

// IncidentRoutingObservedPagerDutyIntegrationSchemaID is the checked-in JSON
// Schema $id for the schema-version-1
// "incident_routing.observed_pagerduty_integration" payload.
const IncidentRoutingObservedPagerDutyIntegrationSchemaID = schemaBaseID + "incident/v1/observed_pagerduty_integration.schema.json"

// IncidentRoutingObservedPagerDutyIntegrationSchema returns the JSON Schema
// bytes for incidentv1.ObservedPagerDutyIntegration.
func IncidentRoutingObservedPagerDutyIntegrationSchema() ([]byte, error) {
	return reflectSchema(IncidentRoutingObservedPagerDutyIntegrationSchemaID, "Eshu incident_routing.observed_pagerduty_integration Payload (schema version 1)", &incidentv1.ObservedPagerDutyIntegration{})
}

// IncidentRoutingCoverageWarningSchemaID is the checked-in JSON Schema $id for
// the schema-version-1 "incident_routing.coverage_warning" payload.
const IncidentRoutingCoverageWarningSchemaID = schemaBaseID + "incident/v1/coverage_warning.schema.json"

// IncidentRoutingCoverageWarningSchema returns the JSON Schema bytes for
// incidentv1.CoverageWarning.
func IncidentRoutingCoverageWarningSchema() ([]byte, error) {
	return reflectSchema(IncidentRoutingCoverageWarningSchemaID, "Eshu incident_routing.coverage_warning Payload (schema version 1)", &incidentv1.CoverageWarning{})
}
