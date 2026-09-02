// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

// TestOpenAPISpecIncludesSecurityAlertReconciliations and
// TestOpenAPISpecIncludesContainerImageIdentities were split out of
// openapi_supply_chain_test.go to keep that file under the repo's 500-line
// cap. MustMapField now lives in querytestutil so the handler-family
// subpackages' tests can reach it (#6060); mustStringSliceField and
// stringSliceContains remain in openapi_supply_chain_test.go and
// code_dead_code_scan_test.go respectively.

func TestOpenAPISpecIncludesSecurityAlertReconciliations(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}

	paths := querytestutil.MustMapField(t, spec, "paths")
	path := querytestutil.MustMapField(t, paths, "/api/v0/supply-chain/security-alerts/reconciliations")
	get := querytestutil.MustMapField(t, path, "get")
	if got, want := get["operationId"], "listSecurityAlertReconciliations"; got != want {
		t.Fatalf("operationId = %#v, want %#v", got, want)
	}
	responses := querytestutil.MustMapField(t, get, "responses")
	twoHundred := querytestutil.MustMapField(t, responses, "200")
	content := querytestutil.MustMapField(t, twoHundred, "content")
	appJSON := querytestutil.MustMapField(t, content, "application/json")
	schema := querytestutil.MustMapField(t, appJSON, "schema")
	properties := querytestutil.MustMapField(t, schema, "properties")
	reconciliations := querytestutil.MustMapField(t, properties, "reconciliations")
	items := querytestutil.MustMapField(t, reconciliations, "items")
	rowProps := querytestutil.MustMapField(t, items, "properties")
	providerAlert := querytestutil.MustMapField(t, rowProps, "provider_alert")
	eshuPackage := querytestutil.MustMapField(t, rowProps, "eshu_package")
	eshuImpact := querytestutil.MustMapField(t, rowProps, "eshu_impact")
	providerProps := querytestutil.MustMapField(t, providerAlert, "properties")
	packageProps := querytestutil.MustMapField(t, eshuPackage, "properties")
	impactProps := querytestutil.MustMapField(t, eshuImpact, "properties")
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
	status := querytestutil.MustMapField(t, rowProps, "reconciliation_status")
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

	paths := querytestutil.MustMapField(t, spec, "paths")
	path := querytestutil.MustMapField(t, paths, "/api/v0/supply-chain/container-images/identities")
	get := querytestutil.MustMapField(t, path, "get")
	if got, want := get["operationId"], "listContainerImageIdentities"; got != want {
		t.Fatalf("operationId = %#v, want %#v", got, want)
	}
}
