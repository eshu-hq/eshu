// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"testing"
)

// TestOpenAPISpecIncludesSecurityAlertReconciliations and
// TestOpenAPISpecIncludesContainerImageIdentities were split out of
// openapi_supply_chain_test.go to keep that file under the repo's 500-line
// cap; they share its mustMapField/mustStringSliceField/stringSliceContains
// helpers, which remain defined in openapi_supply_chain_test.go and
// openapi_test.go/code_dead_code_scan_test.go respectively.

func TestOpenAPISpecIncludesSecurityAlertReconciliations(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}

	paths := mustMapField(t, spec, "paths")
	path := mustMapField(t, paths, "/api/v0/supply-chain/security-alerts/reconciliations")
	get := mustMapField(t, path, "get")
	if got, want := get["operationId"], "listSecurityAlertReconciliations"; got != want {
		t.Fatalf("operationId = %#v, want %#v", got, want)
	}
	responses := mustMapField(t, get, "responses")
	twoHundred := mustMapField(t, responses, "200")
	content := mustMapField(t, twoHundred, "content")
	appJSON := mustMapField(t, content, "application/json")
	schema := mustMapField(t, appJSON, "schema")
	properties := mustMapField(t, schema, "properties")
	reconciliations := mustMapField(t, properties, "reconciliations")
	items := mustMapField(t, reconciliations, "items")
	rowProps := mustMapField(t, items, "properties")
	providerAlert := mustMapField(t, rowProps, "provider_alert")
	eshuPackage := mustMapField(t, rowProps, "eshu_package")
	eshuImpact := mustMapField(t, rowProps, "eshu_impact")
	providerProps := mustMapField(t, providerAlert, "properties")
	packageProps := mustMapField(t, eshuPackage, "properties")
	impactProps := mustMapField(t, eshuImpact, "properties")
	for _, key := range []string{"provider_alert_id", "provider_state", "package_id", "cve_ids", "ghsa_ids"} {
		if _, ok := providerProps[key]; !ok {
			t.Fatalf("provider_alert.properties missing %q", key)
		}
	}
	for _, key := range []string{"observed_version", "requested_range", "dependency_evidence_id", "missing_evidence"} {
		if _, ok := packageProps[key]; !ok {
			t.Fatalf("eshu_package.properties missing %q", key)
		}
	}
	if _, ok := impactProps["impact_status"]; !ok {
		t.Fatalf("eshu_impact.properties missing impact_status")
	}
	status := mustMapField(t, rowProps, "reconciliation_status")
	statusEnum := mustStringSliceField(t, status, "enum")
	for _, want := range []string{"unsupported", "ambiguous"} {
		if !stringSliceContains(statusEnum, want) {
			t.Fatalf("reconciliation_status enum = %#v, want %q", statusEnum, want)
		}
	}
	for _, key := range []string{"reason_code", "missing_evidence"} {
		if _, ok := rowProps[key]; !ok {
			t.Fatalf("reconciliation row properties missing %q", key)
		}
	}
}

func TestOpenAPISpecIncludesContainerImageIdentities(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}

	paths := mustMapField(t, spec, "paths")
	path := mustMapField(t, paths, "/api/v0/supply-chain/container-images/identities")
	get := mustMapField(t, path, "get")
	if got, want := get["operationId"], "listContainerImageIdentities"; got != want {
		t.Fatalf("operationId = %#v, want %#v", got, want)
	}
}
