package mcp

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"testing"
)

func TestDispatchToolServiceStoryCarriesCloudResources(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v0/services/orders-api/story", func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Header.Get("Accept"), "application/eshu.envelope+json"; got != want {
			t.Fatalf("Accept = %q, want %q", got, want)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"service_identity": map[string]any{"service_id": "workload:orders-api"},
				"cloud_resources": []map[string]any{
					{
						"id":                 "cloud-resource:orders-listener",
						"relationship_basis": "aws_resource_service_anchor",
					},
				},
			},
			"truth": map[string]any{"level": "exact"},
			"error": nil,
		})
	})

	result, err := dispatchTool(
		context.Background(),
		mux,
		"get_service_story",
		map[string]any{"workload_id": "workload:orders-api"},
		"",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err != nil {
		t.Fatalf("dispatchTool() error = %v, want nil", err)
	}
	data, ok := result.Envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("Envelope.Data type = %T, want map", result.Envelope.Data)
	}
	resources, ok := data["cloud_resources"].([]any)
	if !ok {
		t.Fatalf("cloud_resources type = %T, want []any", data["cloud_resources"])
	}
	if got, want := len(resources), 1; got != want {
		t.Fatalf("len(cloud_resources) = %d, want %d", got, want)
	}
}
