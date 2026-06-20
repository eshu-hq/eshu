package mcp

import "testing"

func TestFactSchemaVersionToolsResolveToQueryRoutes(t *testing.T) {
	t.Parallel()

	list, err := resolveRoute("list_fact_schema_versions", map[string]any{})
	if err != nil {
		t.Fatalf("resolveRoute(list_fact_schema_versions) error = %v, want nil", err)
	}
	if got, want := list.method, "GET"; got != want {
		t.Fatalf("list method = %q, want %q", got, want)
	}
	if got, want := list.path, "/api/v0/fact-schema-versions"; got != want {
		t.Fatalf("list path = %q, want %q", got, want)
	}
	if got, want := list.query["limit"], "200"; got != want {
		t.Fatalf("list default limit = %q, want %q", got, want)
	}

	bounded, err := resolveRoute("list_fact_schema_versions", map[string]any{"limit": float64(5)})
	if err != nil {
		t.Fatalf("resolveRoute(list bounded) error = %v, want nil", err)
	}
	if got, want := bounded.query["limit"], "5"; got != want {
		t.Fatalf("bounded limit = %q, want %q", got, want)
	}

	detail, err := resolveRoute("get_fact_schema_version", map[string]any{"fact_kind": "terraform_state_resource"})
	if err != nil {
		t.Fatalf("resolveRoute(get_fact_schema_version) error = %v, want nil", err)
	}
	if got, want := detail.path, "/api/v0/fact-schema-versions/terraform_state_resource"; got != want {
		t.Fatalf("detail path = %q, want %q", got, want)
	}
	if detail.query["candidate"] != "" {
		t.Fatalf("detail without candidate has candidate query %q, want empty", detail.query["candidate"])
	}

	classified, err := resolveRoute("get_fact_schema_version", map[string]any{"fact_kind": "terraform_state_resource", "candidate": "2.0.0"})
	if err != nil {
		t.Fatalf("resolveRoute(get_fact_schema_version candidate) error = %v, want nil", err)
	}
	if got, want := classified.query["candidate"], "2.0.0"; got != want {
		t.Fatalf("candidate query = %q, want %q", got, want)
	}

	if _, err := resolveRoute("get_fact_schema_version", map[string]any{}); err == nil {
		t.Fatal("resolveRoute(get_fact_schema_version) without fact_kind = nil error, want error")
	}
}

func TestReadOnlyToolsIncludesFactSchemaVersion(t *testing.T) {
	t.Parallel()

	names := map[string]bool{}
	for _, tool := range ReadOnlyTools() {
		names[tool.Name] = true
	}
	for _, want := range []string{"list_fact_schema_versions", "get_fact_schema_version"} {
		if !names[want] {
			t.Fatalf("ReadOnlyTools() missing %q", want)
		}
	}
}
