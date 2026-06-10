package mcp

import "testing"

func TestListCollectorsRuntimeToolRoutesToStatusCollectors(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("list_collectors", map[string]any{})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "GET"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/status/collectors"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
}

func TestListIngestersRuntimeToolRoutesToStatusIngesters(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("list_ingesters", map[string]any{})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "GET"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/status/ingesters"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
}

func TestGetIngesterStatusRuntimeToolRoutesToRepositoryStatus(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		args map[string]any
		want string
	}{
		{
			name: "default_repository",
			args: map[string]any{},
			want: "/api/v0/status/ingesters/repository",
		},
		{
			name: "explicit_repository",
			args: map[string]any{"ingester": "repository"},
			want: "/api/v0/status/ingesters/repository",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			route, err := resolveRoute("get_ingester_status", tc.args)
			if err != nil {
				t.Fatalf("resolveRoute() error = %v, want nil", err)
			}
			if got, want := route.method, "GET"; got != want {
				t.Fatalf("route.method = %q, want %q", got, want)
			}
			if got := route.path; got != tc.want {
				t.Fatalf("route.path = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSemanticCapabilityRuntimeToolRoutesToStatus(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("get_semantic_capability_status", map[string]any{})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "GET"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/status/semantic-extraction"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
}

func TestHostedReadinessRuntimeToolRoutesToStatus(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("get_hosted_readiness", map[string]any{})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "GET"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/status/hosted-readiness"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
}

func TestHostedGovernanceRuntimeToolRoutesToStatus(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("get_hosted_governance_status", map[string]any{})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "GET"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/status/governance"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
}
