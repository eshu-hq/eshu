// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadRepoDependencyFamilyOduRejectsInvalidMultiScopeShapes(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile(RepoDependencyFamilyCassetteFullPath(repoRootDir(t)))
	if err != nil {
		t.Fatalf("read cassette: %v", err)
	}

	tests := []struct {
		name   string
		want   string
		mutate func(map[string]any)
	}{
		{
			name: "duplicate scope coordinates", want: "repeats scope_id",
			mutate: func(document map[string]any) {
				scopes := repoDependencyTestScopes(document)
				scopes[1]["scope_id"] = scopes[0]["scope_id"]
			},
		},
		{
			name: "duplicate generation coordinates", want: "repeats generation_id",
			mutate: func(document map[string]any) {
				scopes := repoDependencyTestScopes(document)
				scopes[1]["generation_id"] = scopes[0]["generation_id"]
			},
		},
		{
			name: "duplicate repository identity", want: "repeats repository identity",
			mutate: func(document map[string]any) {
				scopes := repoDependencyTestScopes(document)
				firstMetadata := scopes[0]["metadata"].(map[string]any)
				secondMetadata := scopes[1]["metadata"].(map[string]any)
				secondMetadata["repo_id"] = firstMetadata["repo_id"]
			},
		},
		{
			name: "repository fact disagrees with scope metadata", want: "repository fact identity",
			mutate: func(document map[string]any) {
				scopes := repoDependencyTestScopes(document)
				facts := scopes[1]["facts"].([]any)
				payload := facts[0].(map[string]any)["payload"].(map[string]any)
				payload["repo_id"] = scopes[0]["metadata"].(map[string]any)["repo_id"]
			},
		},
		{
			name: "missing repository", want: "repository facts, want exactly 1",
			mutate: func(document map[string]any) {
				scopes := repoDependencyTestScopes(document)
				scopes[0]["facts"] = []any{}
			},
		},
		{
			name: "multiple evidence sources", want: "multiple evidence-bearing source scopes",
			mutate: func(document map[string]any) {
				scopes := repoDependencyTestScopes(document)
				sourceFacts := scopes[len(scopes)-1]["facts"].([]any)
				scopes[0]["facts"] = append(scopes[0]["facts"].([]any), sourceFacts[1:]...)
			},
		},
		{
			name: "evidence source not last", want: "evidence-bearing source scope must be last",
			mutate: func(document map[string]any) {
				scopes := document["scopes"].([]any)
				scopes[0], scopes[len(scopes)-1] = scopes[len(scopes)-1], scopes[0]
			},
		},
		{
			name: "missing production followup", want: "missing production followup facts",
			mutate: func(document map[string]any) {
				scopes := repoDependencyTestScopes(document)
				source := scopes[len(scopes)-1]
				var retained []any
				for _, item := range source["facts"].([]any) {
					fact := item.(map[string]any)
					payload, _ := fact["payload"].(map[string]any)
					if payload["reducer_domain"] != "deployment_mapping" {
						retained = append(retained, item)
					}
				}
				source["facts"] = retained
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var document map[string]any
			if err := json.Unmarshal(raw, &document); err != nil {
				t.Fatalf("unmarshal fixture: %v", err)
			}
			test.mutate(document)
			mutated, err := json.Marshal(document)
			if err != nil {
				t.Fatalf("marshal mutation: %v", err)
			}
			path := filepath.Join(t.TempDir(), "repo-dependency.json")
			if err := os.WriteFile(path, mutated, 0o600); err != nil {
				t.Fatalf("write mutation: %v", err)
			}
			_, err = LoadRepoDependencyFamilyOdu(path)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("loadRepoDependencyFamilyOdu() error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func repoDependencyTestScopes(document map[string]any) []map[string]any {
	rawScopes := document["scopes"].([]any)
	scopes := make([]map[string]any, len(rawScopes))
	for index, scope := range rawScopes {
		scopes[index] = scope.(map[string]any)
	}
	return scopes
}
