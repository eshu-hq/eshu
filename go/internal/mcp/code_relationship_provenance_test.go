package mcp

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
)

func TestCodeRelationshipToolsAdvertiseEdgeResolutionProvenance(t *testing.T) {
	t.Parallel()

	tools := map[string]ToolDefinition{}
	for _, tool := range codebaseTools() {
		tools[tool.Name] = tool
	}
	for _, name := range []string{"get_code_relationship_story", "analyze_code_relationships"} {
		tool, ok := tools[name]
		if !ok {
			t.Fatalf("codebaseTools() missing %q", name)
		}
		description := strings.ToLower(tool.Description)
		for _, want := range []string{"confidence", "resolution_method"} {
			if !strings.Contains(description, want) {
				t.Fatalf("%s description = %q, want %q", name, tool.Description, want)
			}
		}
	}
}

func TestDispatchToolCodeRelationshipStoryPreservesEdgeResolutionProvenance(t *testing.T) {
	t.Parallel()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.URL.Path, "/api/v0/code/relationships/story"; got != want {
			t.Fatalf("path = %q, want %q", got, want)
		}
		query.WriteSuccess(w, r, http.StatusOK, map[string]any{
			"relationships": []map[string]any{
				{
					"direction":         "outgoing",
					"type":              "CALLS",
					"target_id":         "function-callee",
					"target_name":       "callee",
					"confidence":        0.92,
					"resolution_method": "type_inferred",
					"reason":            "Resolved by receiver or return-type inference",
				},
			},
		}, query.BuildTruthEnvelope(
			query.ProfileLocalAuthoritative,
			"call_graph.relationship_story",
			query.TruthBasisAuthoritativeGraph,
			"resolved from bounded relationship story lookup",
		))
	})

	result, err := dispatchTool(
		context.Background(),
		handler,
		"get_code_relationship_story",
		map[string]any{"entity_id": "function-root", "direction": "outgoing"},
		"",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err != nil {
		t.Fatalf("dispatchTool() error = %v, want nil", err)
	}
	if result.Envelope == nil {
		t.Fatal("dispatchTool() envelope is nil, want structured relationship story envelope")
	}
	data, ok := result.Envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("envelope data type = %T, want map[string]any", result.Envelope.Data)
	}
	relationships, ok := data["relationships"].([]any)
	if !ok || len(relationships) != 1 {
		t.Fatalf("relationships = %#v, want one relationship", data["relationships"])
	}
	relationship, ok := relationships[0].(map[string]any)
	if !ok {
		t.Fatalf("relationships[0] type = %T, want map[string]any", relationships[0])
	}
	if got, want := relationship["resolution_method"], "type_inferred"; got != want {
		t.Fatalf("relationship.resolution_method = %#v, want %#v", got, want)
	}
	if got, want := relationship["confidence"], 0.92; got != want {
		t.Fatalf("relationship.confidence = %#v, want %#v", got, want)
	}
}
