// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package facts

import "slices"

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
