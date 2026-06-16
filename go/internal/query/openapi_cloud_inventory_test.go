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
	paths := mustMapField(t, spec, "paths")
	path := mustMapField(t, paths, "/api/v0/cloud/inventory")
	get := mustMapField(t, path, "get")
	if got, want := get["operationId"], "listCloudResourceInventory"; got != want {
		t.Fatalf("operationId = %q, want %q", got, want)
	}
	ok := mustMapField(t, mustMapField(t, get, "responses"), "200")
	schema := mustMapField(t, mustMapField(t, mustMapField(t, ok, "content"), "application/json"), "schema")
	resourceProps := mustMapField(
		t,
		mustMapField(t, mustMapField(t, mustMapField(t, schema, "properties"), "resources"), "items"),
		"properties",
	)
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
