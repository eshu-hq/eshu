package mcp

import "testing"

func TestResolveRouteMapsTerraformImportPlanCandidates(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("propose_terraform_import_plan", map[string]any{
		"account_id":    "123456789012",
		"region":        "us-east-1",
		"finding_kinds": []any{"orphaned_cloud_resource"},
		"limit":         float64(25),
		"offset":        float64(50),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if route.path != "/api/v0/iac/terraform-import-plan/candidates" {
		t.Fatalf("route.path = %q, want /api/v0/iac/terraform-import-plan/candidates", route.path)
	}
	body, ok := route.body.(map[string]any)
	if !ok {
		t.Fatalf("route.body type = %T, want map[string]any", route.body)
	}
	if got, want := body["account_id"], "123456789012"; got != want {
		t.Fatalf("body[account_id] = %#v, want %#v", got, want)
	}
	if got, want := body["region"], "us-east-1"; got != want {
		t.Fatalf("body[region] = %#v, want %#v", got, want)
	}
	if got, want := body["limit"], 25; got != want {
		t.Fatalf("body[limit] = %#v, want %#v", got, want)
	}
	if got, want := body["offset"], 50; got != want {
		t.Fatalf("body[offset] = %#v, want %#v", got, want)
	}
	kinds := body["finding_kinds"].([]any)
	if len(kinds) != 1 || kinds[0] != "orphaned_cloud_resource" {
		t.Fatalf("finding_kinds = %#v, want orphaned_cloud_resource", kinds)
	}
}
