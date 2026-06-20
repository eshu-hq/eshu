package mcp

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
)

func TestResolveRouteMapsInvestigationPacketTools(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		toolName  string
		args      map[string]any
		wantPath  string
		wantQuery map[string]string
	}{
		{
			name:     "supply chain impact",
			toolName: "export_supply_chain_impact_packet",
			args: map[string]any{
				"advisory_id":      "GHSA-test",
				"package_id":       "pkg:npm/left-pad",
				"repository_id":    "repo://team/api",
				"subject_digest":   "sha256:abc",
				"max_source_facts": float64(12),
			},
			wantPath: "/api/v0/investigations/supply-chain/impact/packet",
			wantQuery: map[string]string{
				"advisory_id":      "GHSA-test",
				"package_id":       "pkg:npm/left-pad",
				"repository_id":    "repo://team/api",
				"subject_digest":   "sha256:abc",
				"max_source_facts": "12",
			},
		},
		{
			name:     "deployable unit",
			toolName: "export_deployable_unit_packet",
			args: map[string]any{
				"scope_id":         "git-repository-scope:team/api",
				"generation_id":    "generation-1",
				"repository_id":    "repo://team/api",
				"max_source_facts": 8,
			},
			wantPath: "/api/v0/investigations/deployable-unit/packet",
			wantQuery: map[string]string{
				"scope_id":         "git-repository-scope:team/api",
				"generation_id":    "generation-1",
				"repository_id":    "repo://team/api",
				"max_source_facts": "8",
			},
		},
		{
			name:     "runtime drift",
			toolName: "export_cloud_runtime_drift_packet",
			args: map[string]any{
				"project_id":         "project-synthetic",
				"provider":           "gcp",
				"cloud_resource_uid": "gcp:project-synthetic:compute.googleapis.com/Instance:vm-1",
				"max_source_facts":   4,
			},
			wantPath: "/api/v0/investigations/drift/packet",
			wantQuery: map[string]string{
				"project_id":         "project-synthetic",
				"provider":           "gcp",
				"cloud_resource_uid": "gcp:project-synthetic:compute.googleapis.com/Instance:vm-1",
				"max_source_facts":   "4",
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			route, err := resolveRoute(tt.toolName, tt.args)
			if err != nil {
				t.Fatalf("resolveRoute() error = %v, want nil", err)
			}
			if got, want := route.method, "GET"; got != want {
				t.Fatalf("route.method = %q, want %q", got, want)
			}
			if got := route.path; got != tt.wantPath {
				t.Fatalf("route.path = %q, want %q", got, tt.wantPath)
			}
			for key, want := range tt.wantQuery {
				if got := route.query[key]; got != want {
					t.Fatalf("route.query[%s] = %#v, want %#v", key, got, want)
				}
			}
		})
	}
}

func TestInvestigationPacketToolsAdvertiseRequiredInputs(t *testing.T) {
	t.Parallel()

	for _, name := range []string{
		"export_supply_chain_impact_packet",
		"export_deployable_unit_packet",
		"export_cloud_runtime_drift_packet",
	} {
		tool := requireToolDefinition(t, name)
		schema, ok := tool.InputSchema.(map[string]any)
		if !ok {
			t.Fatalf("tool %s InputSchema type = %T, want map[string]any", name, tool.InputSchema)
		}
		properties, ok := schema["properties"].(map[string]any)
		if !ok {
			t.Fatalf("tool %s properties type = %T, want map[string]any", name, schema["properties"])
		}
		if _, ok := properties["max_source_facts"]; !ok {
			t.Fatalf("tool %s missing max_source_facts property", name)
		}
	}
}

func TestDispatchToolInvestigationPacketReturnsHTTPEnvelopePacket(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v0/investigations/supply-chain/impact/packet", func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Header.Get("Accept"), query.EnvelopeMIMEType; got != want {
			t.Fatalf("Accept = %q, want %q", got, want)
		}
		if got, want := r.URL.Query().Get("finding_id"), "finding-1"; got != want {
			t.Fatalf("finding_id = %q, want %q", got, want)
		}
		if got, want := r.URL.Query().Get("max_source_facts"), "3"; got != want {
			t.Fatalf("max_source_facts = %q, want %q", got, want)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"schema":    "investigation_evidence_packet.v2",
				"packet_id": "packet-1",
				"refusal":   "none",
			},
			"truth": map[string]any{
				"level":      "exact",
				"capability": "supply_chain.impact_explanation.read",
				"profile":    "production",
			},
			"error": nil,
		})
	})

	result, err := dispatchTool(
		context.Background(),
		mux,
		"export_supply_chain_impact_packet",
		map[string]any{"finding_id": "finding-1", "max_source_facts": 3},
		"Bearer token",
		slog.Default(),
	)
	if err != nil {
		t.Fatalf("dispatchTool() error = %v, want nil", err)
	}
	if result.Envelope == nil {
		t.Fatal("dispatchTool() envelope is nil, want canonical packet envelope")
	}
	data, ok := result.Envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("envelope data type = %T, want map[string]any", result.Envelope.Data)
	}
	if got, want := data["schema"], "investigation_evidence_packet.v2"; got != want {
		t.Fatalf("schema = %v, want %v", got, want)
	}
	if got, want := data["packet_id"], "packet-1"; got != want {
		t.Fatalf("packet_id = %v, want %v", got, want)
	}
	if result.Envelope.Truth == nil || result.Envelope.Truth.Capability != "supply_chain.impact_explanation.read" {
		t.Fatalf("truth = %#v, want supply-chain impact packet truth", result.Envelope.Truth)
	}
}
