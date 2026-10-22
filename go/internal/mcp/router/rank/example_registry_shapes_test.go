// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package rank

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/mcp/contract/tool"
	"github.com/eshu-hq/eshu/go/internal/mcp/router"
)

// registryShapeTools mirrors the input schemas of three real registry tools
// whose required properties are []string enums. The schemas are copied by shape:
// the enum is []string, "required" is []string, and properties are
// map[string]any. A survey of the registry found 88 []string enums and 2 []any
// enums, and six required enum properties, so []string is the common case.
func registryShapeTools() []toolcontract.ToolDefinition {
	return []toolcontract.ToolDefinition{
		{
			Name:        "analyze_infra_relationships",
			Description: "Analyze infrastructure relationships: what deploys what, what provisions what.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query_type": map[string]any{
						"type":        "string",
						"description": "Type of infrastructure relationship to analyze",
						"enum":        []string{"what_deploys", "what_provisions", "who_consumes_xrd", "module_consumers", "what_runs_image", "what_runs_lambda_image"},
					},
					"target": map[string]any{"type": "string", "description": "Target infrastructure entity"},
				},
				"required": []string{"query_type", "target"},
			},
		},
		{
			Name:        "derive_visualization_packet",
			Description: "Derive a bounded visualization packet from an already-authorized answer response.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"view": map[string]any{
						"type":        "string",
						"description": "Visualization view to derive.",
						"enum":        []string{"service_story", "evidence_citation", "incident_context"},
					},
					"source_response": map[string]any{"type": "object", "description": "Source answer response payload."},
					"source_truth":    map[string]any{"type": "object", "description": "Optional source TruthEnvelope."},
				},
				"required": []string{"view", "source_response"},
			},
		},
		{
			Name:        "investigate_code_divergence",
			Description: "Drill into one divergence finding addressed by kind and fingerprint.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"repo_id": map[string]any{"type": "string", "description": "Canonical repository identifier"},
					"kind": map[string]any{
						"type":        "string",
						"description": "Divergence family",
						"enum":        []string{"exact", "renamed", "drifted", "wrapper_bypass", "convention_outlier", "parallel_implementation.exact"},
					},
					"fingerprint":   map[string]any{"type": "string", "description": "Finding fingerprint"},
					"include_tests": map[string]any{"type": "boolean", "description": "Opt test-file copies back in", "default": false},
				},
				"required": []string{"repo_id", "kind", "fingerprint"},
			},
		},
	}
}

func TestExampleOnRegistryShapedSchemas(t *testing.T) {
	tools := registryShapeTools()
	want := map[string]map[string]any{
		"analyze_infra_relationships": {"query_type": "what_deploys", "target": "<target>"},
		"derive_visualization_packet": {"view": "service_story", "source_response": map[string]any{}},
		"investigate_code_divergence": {"repo_id": "repo-7", "kind": "exact", "fingerprint": "<fingerprint>", "include_tests": false},
	}
	q := router.Query{
		Intent:  "analyze derive visualization infrastructure relationships divergence finding",
		Limit:   router.MaxLimit,
		Anchors: router.Anchors{RepoID: "repo-7"},
	}
	got := rankWith(t, tools, nil, q)
	if len(got) != len(want) {
		t.Fatalf("got %v, want all three tools", names(got))
	}
	for _, c := range got {
		if !reflect.DeepEqual(c.Example, want[c.Name]) {
			t.Errorf("%s Example = %#v, want %#v", c.Name, c.Example, want[c.Name])
		}
	}
}
