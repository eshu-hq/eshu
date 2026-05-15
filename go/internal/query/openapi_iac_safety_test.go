package query

import (
	"encoding/json"
	"testing"
)

func TestOpenAPIIaCManagementSafetyGateFields(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}
	paths := mustMapField(t, spec, "paths")

	unmanagedPath := mustMapField(t, paths, "/api/v0/iac/unmanaged-resources")
	unmanagedPost := mustMapField(t, unmanagedPath, "post")
	unmanagedOK := mustMapField(t, mustMapField(t, unmanagedPost, "responses"), "200")
	unmanagedProps := mustMapField(
		t,
		mustMapField(t, mustMapField(t, mustMapField(t, unmanagedOK, "content"), "application/json"), "schema"),
		"properties",
	)
	if _, ok := unmanagedProps["safety_summary"]; !ok {
		t.Fatal("iac/unmanaged-resources response schema missing safety_summary")
	}
	findings := mustMapField(t, unmanagedProps, "findings")
	findingProps := mustMapField(t, mustMapField(t, findings, "items"), "properties")
	if _, ok := findingProps["safety_gate"]; !ok {
		t.Fatal("iac/unmanaged-resources finding schema missing safety_gate")
	}

	statusPath := mustMapField(t, paths, "/api/v0/iac/management-status")
	statusPost := mustMapField(t, statusPath, "post")
	statusOK := mustMapField(t, mustMapField(t, statusPost, "responses"), "200")
	statusProps := mustMapField(
		t,
		mustMapField(t, mustMapField(t, mustMapField(t, statusOK, "content"), "application/json"), "schema"),
		"properties",
	)
	if _, ok := statusProps["safety_gate"]; !ok {
		t.Fatal("iac/management-status response schema missing safety_gate")
	}

	explainPath := mustMapField(t, paths, "/api/v0/iac/management-status/explain")
	explainPost := mustMapField(t, explainPath, "post")
	explainOK := mustMapField(t, mustMapField(t, explainPost, "responses"), "200")
	explainProps := mustMapField(
		t,
		mustMapField(t, mustMapField(t, mustMapField(t, explainOK, "content"), "application/json"), "schema"),
		"properties",
	)
	if _, ok := explainProps["safety_gate"]; !ok {
		t.Fatal("iac/management-status/explain response schema missing safety_gate")
	}
}
