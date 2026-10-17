// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package facts

import (
	"slices"

	"github.com/eshu-hq/eshu/go/internal/facts/contract"
)

const (
	// IncidentRecordFactKind identifies one provider-reported operational
	// incident. It is source evidence only; reducers own incident context
	// graph and read-model truth.
	IncidentRecordFactKind = "incident.record"
	// IncidentLifecycleEventFactKind identifies one provider-reported incident
	// timeline or log event.
	IncidentLifecycleEventFactKind = "incident.lifecycle_event"
	// ChangeRecordFactKind identifies one provider-reported operational change
	// event related to an incident or service.
	ChangeRecordFactKind = "change.record"

	// IncidentContextSchemaVersionV1 is the first incident-context source fact
	// schema.
	IncidentContextSchemaVersionV1 = "1.0.0"
)

var incidentContextFactKinds = []string{
	IncidentRecordFactKind,
	IncidentLifecycleEventFactKind,
	ChangeRecordFactKind,
}

var incidentContextSchemaVersions = map[string]string{
	IncidentRecordFactKind:         IncidentContextSchemaVersionV1,
	IncidentLifecycleEventFactKind: IncidentContextSchemaVersionV1,
	ChangeRecordFactKind:           IncidentContextSchemaVersionV1,
}

// IncidentContextFactKinds returns accepted incident-context source fact kinds
// in source-contract order.
func IncidentContextFactKinds() []string {
	return slices.Clone(incidentContextFactKinds)
}

// IncidentContextSchemaVersion returns the schema version for an
// incident-context source fact kind.
func IncidentContextSchemaVersion(factKind string) (string, bool) {
	version, ok := incidentContextSchemaVersions[factKind]
	return version, ok
}

// IncidentContextFamilyDeclaration is the typed fact-family contract declaration for the
// incident_context family (issue #6629). It is build-time metadata: the
// fact-kind-registry generator discovers it statically and derives
// the registry, shards, and catalog from it.
var IncidentContextFamilyDeclaration = contract.Declare(contract.Family{
	ID:                     "incident_context",
	FactDir:                "go/internal/facts",
	OwnerRepository:        "eshu-hq/eshu",
	LifecycleOwner:         "go/internal/facts",
	SchemaVersion:          "1.0.0",
	ReducerDomain:          "incident_repository_correlation",
	ProjectionHook:         "incident_context_read_model",
	AdmissionHook:          "facts.ValidateSchemaVersion",
	ReadSurface:            "GET /api/v0/incidents/{incident_id}/context",
	TruthProfile:           "provider_gated",
	ProviderKeyIndependent: false,
	Kinds: []contract.Kind{
		{Kind: "change.record", PayloadSchema: "sdk/go/factschema/schema/change.record.v1.schema.json", Disposition: "read_only", NoReducerReason: "Consumed by read surfaces only; no reducer domain decodes it.", NoReducerEvidence: "read consumption in go/internal/query/incident/store"},
		{Kind: "incident.lifecycle_event", PayloadSchema: "sdk/go/factschema/schema/incident.lifecycle_event.v1.schema.json", Disposition: "read_only", NoReducerReason: "Consumed by read surfaces only; no reducer domain decodes it.", NoReducerEvidence: "read consumption in go/internal/query/incident/store"},
		{Kind: "incident.record", PayloadSchema: "sdk/go/factschema/schema/incident.record.v1.schema.json"},
	},
	Producers: []contract.Producer{
		{ID: "collector-pagerduty", Repository: "eshu-hq/eshu", Path: "go/internal/collector/pagerduty", PlannedRepository: "eshu-hq/eshu-collector-pagerduty", AuthorityMode: "core_issued_grant", Kinds: []string{"change.record", "incident.lifecycle_event", "incident.record"}},
	},
})
