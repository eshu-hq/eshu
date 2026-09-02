// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

func TestOpenAPICloudInventoryDocumentsIdentityPolicyEvidence(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}
	resourceProps := cloudInventoryOpenAPIResourceProperties(t, spec)
	if _, present := resourceProps["tag_value_fingerprints"]; !present {
		t.Fatal("cloud inventory resource schema missing tag_value_fingerprints")
	}
	if _, present := resourceProps["identity_policy_evidence_truncated"]; !present {
		t.Fatal("cloud inventory resource schema missing identity_policy_evidence_truncated")
	}
	identityEvidence := querytestutil.MustMapField(t, resourceProps, "identity_policy_evidence")
	identityProps := querytestutil.MustMapField(t, querytestutil.MustMapField(t, identityEvidence, "items"), "properties")
	for _, field := range []string{
		"evidence_key",
		"identity_type",
		"role_class",
		"principal_fingerprint",
		"client_fingerprint",
		"object_fingerprint",
		"tenant_fingerprint",
	} {
		if _, present := identityProps[field]; !present {
			t.Fatalf("identity_policy_evidence schema missing %q", field)
		}
	}
}

func TestOpenAPICloudInventoryDocumentsResourceChangeFreshness(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}
	resourceProps := cloudInventoryOpenAPIResourceProperties(t, spec)
	freshness := querytestutil.MustMapField(t, resourceProps, "resource_change_freshness")
	if got, want := freshness["description"], "Optional sanitized Azure Resource Graph change evidence attached to an already-admitted canonical resource. Delete rows are tombstone candidates only."; got != want {
		t.Fatalf("resource_change_freshness description = %q, want %q", got, want)
	}
	freshnessProps := querytestutil.MustMapField(t, querytestutil.MustMapField(t, freshness, "items"), "properties")
	for _, field := range []string{
		"evidence_key",
		"change_type",
		"change_time",
		"operation",
		"client_type",
		"actor_class",
		"actor_fingerprint",
		"changed_property_paths",
		"changed_property_truncated",
		"tombstone_candidate",
	} {
		if _, present := freshnessProps[field]; !present {
			t.Fatalf("resource_change_freshness schema missing %q", field)
		}
	}
	truncated := querytestutil.MustMapField(t, resourceProps, "resource_change_freshness_truncated")
	if got, want := truncated["type"], "boolean"; got != want {
		t.Fatalf("resource_change_freshness_truncated type = %q, want %q", got, want)
	}
}

// TestOpenAPICloudInventoryDocumentsCodeSHA256Correlation keeps the OpenAPI
// schema in lockstep with the readback's #5454 code_sha256_correlation label:
// the zip-Lambda deployment-code correlation limitation must be documented on
// the resource schema with its bounded status/truth_basis/unsupported_reason
// enum values so the wire contract advertises the gap, never leaving it silent.
func TestOpenAPICloudInventoryDocumentsCodeSHA256Correlation(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}
	resourceProps := cloudInventoryOpenAPIResourceProperties(t, spec)
	correlation := querytestutil.MustMapField(t, resourceProps, cloudInventoryCodeCorrelationKey)
	props := querytestutil.MustMapField(t, correlation, "properties")
	wantEnums := map[string]string{
		"status":             cloudInventoryCodeCorrelationStatusUncorrelated,
		"truth_basis":        cloudInventoryCodeCorrelationTruthBasisDisplayOnly,
		"unsupported_reason": cloudInventoryZipCodeSHA256UnsupportedReason,
	}
	for field, wantValue := range wantEnums {
		fieldSchema := querytestutil.MustMapField(t, props, field)
		enum, ok := fieldSchema["enum"].([]any)
		if !ok || len(enum) != 1 || enum[0] != wantValue {
			t.Fatalf("%s.%s enum = %#v, want [%q]", cloudInventoryCodeCorrelationKey, field, fieldSchema["enum"], wantValue)
		}
	}
}

func cloudInventoryOpenAPIResourceProperties(t *testing.T, spec map[string]any) map[string]any {
	t.Helper()

	paths := querytestutil.MustMapField(t, spec, "paths")
	path := querytestutil.MustMapField(t, paths, "/api/v0/cloud/inventory")
	get := querytestutil.MustMapField(t, path, "get")
	if got, want := get["operationId"], "listCloudResourceInventory"; got != want {
		t.Fatalf("operationId = %q, want %q", got, want)
	}
	okResponse := querytestutil.MustMapField(t, querytestutil.MustMapField(t, get, "responses"), "200")
	schema := querytestutil.MustMapField(
		t,
		querytestutil.MustMapField(t, querytestutil.MustMapField(t, okResponse, "content"), "application/json"),
		"schema",
	)
	return querytestutil.MustMapField(
		t,
		querytestutil.MustMapField(t, querytestutil.MustMapField(t, querytestutil.MustMapField(t, schema, "properties"), "resources"), "items"),
		"properties",
	)
}
