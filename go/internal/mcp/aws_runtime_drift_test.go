package mcp

import (
	"strings"
	"testing"
)

func TestResolveRouteMapsAWSRuntimeDriftFindings(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("list_aws_runtime_drift_findings", map[string]any{
		"account_id":    "123456789012",
		"region":        "us-east-1",
		"finding_kinds": []any{"unmanaged_cloud_resource"},
		"limit":         float64(25),
		"offset":        float64(50),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if route.path != "/api/v0/aws/runtime-drift/findings" {
		t.Fatalf("route.path = %q, want /api/v0/aws/runtime-drift/findings", route.path)
	}
	body, ok := route.body.(map[string]any)
	if !ok {
		t.Fatalf("route.body type = %T, want map[string]any", route.body)
	}
	if got, want := body["account_id"], "123456789012"; got != want {
		t.Fatalf("body[account_id] = %#v, want %#v", got, want)
	}
	if got, want := body["limit"], 25; got != want {
		t.Fatalf("body[limit] = %#v, want %#v", got, want)
	}
	kinds := body["finding_kinds"].([]any)
	if len(kinds) != 1 || kinds[0] != "unmanaged_cloud_resource" {
		t.Fatalf("finding_kinds = %#v, want unmanaged_cloud_resource", kinds)
	}
}

func TestAWSRuntimeDriftFindingsSchemaDocumentsScope(t *testing.T) {
	t.Parallel()

	tool := awsRuntimeDriftFindingsTool()
	schema := tool.InputSchema.(map[string]any)
	if _, ok := schema["anyOf"]; ok {
		t.Fatal("schema must not advertise top-level anyOf")
	}
	if !strings.Contains(tool.Description, "Provide scope_id or account_id") {
		t.Fatalf("tool description = %q, want scope guidance", tool.Description)
	}
}
