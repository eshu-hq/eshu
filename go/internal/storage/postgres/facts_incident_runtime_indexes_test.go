// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
)

func TestBootstrapDefinitionsIncludeIncidentRuntimeReadIndexes(t *testing.T) {
	t.Parallel()

	var facts Definition
	for _, def := range BootstrapDefinitions() {
		if def.Name == "fact_records" {
			facts = def
			break
		}
	}
	if facts.Name == "" {
		t.Fatal("fact_records definition missing")
	}
	for _, want := range []string{
		"fact_records_service_catalog_operational_link_url_idx",
		"fact_records_kubernetes_correlation_image_lookup_idx",
		"fact_records_incident_repository_correlation_service_idx",
		"fact_records_incident_record_provider_service_idx",
		"fact_records_incident_routing_applied_service_idx",
		"fact_records_incident_routing_observed_service_idx",
		"fact_kind = 'service_catalog.operational_link'",
		"fact_kind = 'reducer_kubernetes_correlation'",
		"fact_kind = 'reducer_incident_repository_correlation'",
		"fact_kind = 'incident.record'",
		"fact_kind = 'incident_routing.applied_pagerduty_resource'",
		"fact_kind = 'incident_routing.observed_pagerduty_service'",
		"(payload->>'source_digest')",
		"(payload->>'image_ref')",
		"(payload->>'provider_service_id')",
		"payload->'service'->>'id'",
		"(payload->>'provider_object_id')",
	} {
		if !strings.Contains(facts.SQL, want) {
			t.Fatalf("Bootstrap SQL missing incident runtime index fragment %q", want)
		}
	}
}
