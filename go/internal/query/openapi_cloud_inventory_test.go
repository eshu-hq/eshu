// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"testing"
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
	identityEvidence := mustMapField(t, resourceProps, "identity_policy_evidence")
	identityProps := mustMapField(t, mustMapField(t, identityEvidence, "items"), "properties")
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
	freshness := mustMapField(t, resourceProps, "resource_change_freshness")
	if got, want := freshness["description"], "Optional sanitized Azure Resource Graph change evidence attached to an already-admitted canonical resource. Delete rows are tombstone candidates only."; got != want {
		t.Fatalf("resource_change_freshness description = %q, want %q", got, want)
	}
	freshnessProps := mustMapField(t, mustMapField(t, freshness, "items"), "properties")
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
	truncated := mustMapField(t, resourceProps, "resource_change_freshness_truncated")
	if got, want := truncated["type"], "boolean"; got != want {
		t.Fatalf("resource_change_freshness_truncated type = %q, want %q", got, want)
	}
}

func cloudInventoryOpenAPIResourceProperties(t *testing.T, spec map[string]any) map[string]any {
	t.Helper()

	paths := mustMapField(t, spec, "paths")
	path := mustMapField(t, paths, "/api/v0/cloud/inventory")
	get := mustMapField(t, path, "get")
	if got, want := get["operationId"], "listCloudResourceInventory"; got != want {
		t.Fatalf("operationId = %q, want %q", got, want)
	}
	okResponse := mustMapField(t, mustMapField(t, get, "responses"), "200")
	schema := mustMapField(
		t,
		mustMapField(t, mustMapField(t, okResponse, "content"), "application/json"),
		"schema",
	)
	return mustMapField(
		t,
		mustMapField(t, mustMapField(t, mustMapField(t, schema, "properties"), "resources"), "items"),
		"properties",
	)
}
