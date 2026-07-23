// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/content"
	"github.com/eshu-hq/eshu/go/internal/repositoryidentity"
)

func TestGoldenSnapshotGitHubActionsEntityContextShape(t *testing.T) {
	t.Parallel()

	snap, err := LoadSnapshot(goldenSnapshotPath())
	if err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}
	repoID, err := repositoryidentity.CanonicalRepositoryID("https://github.com/acme/github_actions_workflows", "")
	if err != nil {
		t.Fatalf("CanonicalRepositoryID() error = %v", err)
	}
	entityID := content.CanonicalEntityID(repoID, ".github/workflows/ci.yml", "File", "ci", 1)

	mcpShape, ok := snap.QueryShapes.MCP["get_entity_context"]
	if !ok {
		t.Fatal("query_shapes.mcp missing get_entity_context")
	}
	if got := mcpShape.Arguments["entity_id"]; got != entityID {
		t.Fatalf("get_entity_context entity_id = %v, want %q", got, entityID)
	}
	httpKey := "GET /api/v0/entities/" + entityID + "/context"
	httpShape, ok := snap.QueryShapes.HTTP[httpKey]
	if !ok {
		t.Fatalf("query_shapes.http missing canonical workflow context %q", httpKey)
	}

	valid := []byte(`{
		"id":"` + entityID + `",
		"entity_id":"` + entityID + `",
		"relationships":[{"type":"DEPENDS_ON","target_name":"hashicorp/setup-terraform","reason":"github_actions_action_repository"}],
		"result_limits":{"relationship_count":1,"truncated":false},
		"partial_reasons":[]
	}`)
	for name, shape := range map[string]QueryShape{"http": httpShape, "mcp": mcpShape} {
		if finding := EvaluateQueryShape(name+":github-actions-context", shape, valid); !finding.OK {
			t.Fatalf("%s valid workflow context failed: %s", name, finding.Detail)
		}
	}

	mutated := []byte(`{
		"id":"` + entityID + `",
		"entity_id":"` + entityID + `",
		"relationships":[
			{"type":"DEPENDS_ON","target_name":"hashicorp/setup-terraform","reason":"github_actions_action_repository"},
			{"type":"DEPENDS_ON","target_name":"octocat/example-action","reason":"github_actions_action_repository"}
		],
		"result_limits":{"relationship_count":2,"truncated":false},
		"partial_reasons":[]
	}`)
	for name, shape := range map[string]QueryShape{"http": httpShape, "mcp": mcpShape} {
		if finding := EvaluateQueryShape(name+":github-actions-context-mutated", shape, mutated); finding.OK {
			t.Fatalf("%s accepted run-block octocat foil and relationship_count=2", name)
		}
	}
}
