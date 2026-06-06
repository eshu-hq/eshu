package mcp

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"testing"
)

func TestDispatchToolServiceStoryPreservesSpecCountConsistency(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v0/services/sample-service-api/story", func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Header.Get("Accept"), "application/eshu.envelope+json"; got != want {
			t.Fatalf("Accept = %q, want %q", got, want)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"api_surface":      map[string]any{"spec_count": 2, "spec_paths": []string{"openapi.yaml", "admin.yaml"}},
				"support_overview": map[string]any{"spec_count": 2},
			},
			"truth": map[string]any{
				"level":      "exact",
				"capability": "platform_impact.context_overview",
				"profile":    "production",
				"basis":      "hybrid",
				"freshness":  map[string]any{"state": "fresh"},
			},
			"error": nil,
		})
	})

	result, err := dispatchTool(
		context.Background(),
		mux,
		"get_service_story",
		map[string]any{"workload_id": "workload:sample-service-api"},
		"",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err != nil {
		t.Fatalf("dispatchTool() error = %v, want nil", err)
	}
	if result.Envelope == nil {
		t.Fatal("dispatchTool() envelope is nil, want structured service story envelope")
	}
	data, ok := result.Envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("envelope data type = %T, want object", result.Envelope.Data)
	}
	apiSurface, ok := data["api_surface"].(map[string]any)
	if !ok {
		t.Fatalf("api_surface type = %T, want object", data["api_surface"])
	}
	supportOverview, ok := data["support_overview"].(map[string]any)
	if !ok {
		t.Fatalf("support_overview type = %T, want object", data["support_overview"])
	}
	if got, want := serviceStoryTestIntValue(apiSurface, "spec_count"), 2; got != want {
		t.Fatalf("api_surface.spec_count = %d, want %d", got, want)
	}
	if got, want := serviceStoryTestIntValue(supportOverview, "spec_count"), serviceStoryTestIntValue(apiSurface, "spec_count"); got != want {
		t.Fatalf("support_overview.spec_count = %d, want api_surface.spec_count %d", got, want)
	}
}

func serviceStoryTestIntValue(row map[string]any, key string) int {
	switch value := row[key].(type) {
	case int:
		return value
	case float64:
		return int(value)
	default:
		return 0
	}
}
