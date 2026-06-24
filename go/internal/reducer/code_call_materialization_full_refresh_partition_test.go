// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestCodeCallMaterializationHandlerPartitionsFullRefreshByDurableFileOwnership(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 15, 18, 0, 0, 0, time.UTC)
	loader := &stubFactLoader{
		envelopes: []facts.Envelope{
			{
				FactKind: factKindRepository,
				Payload: map[string]any{
					"repo_id":       "repo-a",
					"source_run_id": "run-a",
					"graph_id":      "repo-a",
					"path":          "/repo",
				},
			},
			{
				FactKind: factKindRepository,
				Payload: map[string]any{
					"repo_id":       "repo-b",
					"source_run_id": "run-b",
					"graph_id":      "repo-b",
				},
			},
			{
				FactKind: factKindFile,
				Payload: map[string]any{
					"repo_id":       "repo-a",
					"relative_path": "caller.py",
					"parsed_file_data": map[string]any{
						"path": "caller.py",
						"functions": []any{
							map[string]any{
								"name":        "handle",
								"line_number": 3,
								"uid":         "entity:handle",
							},
						},
						"function_calls_scip": []any{
							map[string]any{
								"caller_file":   "caller.py",
								"caller_line":   3,
								"caller_symbol": "pkg/caller#handle().",
								"callee_file":   "callee.py",
								"callee_line":   1,
								"callee_symbol": "pkg/callee#callee().",
							},
						},
					},
				},
			},
			{
				FactKind: factKindFile,
				Payload: map[string]any{
					"repo_id":       "repo-a",
					"relative_path": "callee.py",
					"parsed_file_data": map[string]any{
						"path": "callee.py",
						"functions": []any{
							map[string]any{
								"name":        "callee",
								"line_number": 1,
								"uid":         "entity:callee",
							},
						},
					},
				},
			},
			{
				FactKind: factKindFile,
				Payload: map[string]any{
					"repo_id":       "repo-a",
					"relative_path": "models.py",
					"parsed_file_data": map[string]any{
						"path": "models.py",
						"classes": []any{
							map[string]any{
								"name":      "Widget",
								"uid":       "entity:widget",
								"metaclass": "Meta",
							},
							map[string]any{
								"name": "Meta",
								"uid":  "entity:meta",
							},
						},
					},
				},
			},
		},
	}
	writer := &recordingCodeCallIntentWriter{}
	handler := CodeCallMaterializationHandler{
		FactLoader:   loader,
		IntentWriter: writer,
	}

	result, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-code-call-full-refresh",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		SourceSystem: "git",
		Domain:       DomainCodeCallMaterialization,
		EnqueuedAt:   now,
		AvailableAt:  now,
		Status:       IntentStatusPending,
	})
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if result.Status != ResultStatusSucceeded {
		t.Fatalf("result.Status = %q, want %q", result.Status, ResultStatusSucceeded)
	}

	var repoARefresh SharedProjectionIntentRow
	var repoBRefresh SharedProjectionIntentRow
	var codeCallRow SharedProjectionIntentRow
	var metaclassRow SharedProjectionIntentRow
	for _, row := range writer.rows {
		switch {
		case row.RepositoryID == "repo-a" && row.Payload["intent_type"] == "repo_refresh":
			repoARefresh = row
		case row.RepositoryID == "repo-b" && row.Payload["intent_type"] == "repo_refresh":
			repoBRefresh = row
		case row.Payload["evidence_source"] == codeCallEvidenceSource:
			codeCallRow = row
		case row.Payload["evidence_source"] == pythonMetaclassEvidenceSource:
			metaclassRow = row
		}
	}

	if repoARefresh.IntentID == "" {
		t.Fatal("missing repo-a refresh row")
	}
	if repoBRefresh.IntentID == "" {
		t.Fatal("missing repo-b refresh row")
	}
	if codeCallRow.IntentID == "" {
		t.Fatal("missing code-call row")
	}
	if metaclassRow.IntentID == "" {
		t.Fatal("missing metaclass row")
	}

	wantRefreshPaths := []string{"/repo/callee.py", "/repo/caller.py", "/repo/models.py"}
	if gotPaths := semanticPayloadStringSlice(repoARefresh.Payload, "delta_file_paths"); !reflect.DeepEqual(gotPaths, wantRefreshPaths) {
		t.Fatalf("repo-a refresh delta_file_paths = %#v, want %#v", gotPaths, wantRefreshPaths)
	}
	if got, want := repoARefresh.PartitionKey, codeCallRefreshPartitionKeyForDelta("repo-a", []string{"callee.py", "caller.py", "models.py"}); got != want {
		t.Fatalf("repo-a refresh PartitionKey = %q, want %q", got, want)
	}
	if got, want := codeCallRow.PartitionKey, codeCallRefreshPartitionKeyForDelta("repo-a", []string{"caller.py"}); got != want {
		t.Fatalf("code-call PartitionKey = %q, want %q", got, want)
	}
	if gotPaths := semanticPayloadStringSlice(codeCallRow.Payload, "delta_file_paths"); !reflect.DeepEqual(gotPaths, []string{"/repo/caller.py"}) {
		t.Fatalf("code-call delta_file_paths = %#v, want [/repo/caller.py]", gotPaths)
	}
	if got, want := metaclassRow.PartitionKey, codeCallRefreshPartitionKeyForDelta("repo-a", []string{"models.py"}); got != want {
		t.Fatalf("metaclass PartitionKey = %q, want %q", got, want)
	}
	if gotPaths := semanticPayloadStringSlice(metaclassRow.Payload, "delta_file_paths"); !reflect.DeepEqual(gotPaths, []string{"/repo/models.py"}) {
		t.Fatalf("metaclass delta_file_paths = %#v, want [/repo/models.py]", gotPaths)
	}
	if got, want := repoBRefresh.PartitionKey, codeCallRefreshPartitionKey("repo-b"); got != want {
		t.Fatalf("repo-b refresh PartitionKey = %q, want whole-scope %q", got, want)
	}
}

func TestBuildCodeCallFileScopesFallsBackForUnsafeFullRefreshOwnership(t *testing.T) {
	t.Parallel()

	result := buildCodeCallFileScopesByRepoID([]facts.Envelope{
		{
			FactKind: factKindRepository,
			Payload: map[string]any{
				"repo_id":       "repo-a",
				"source_run_id": "run-a",
				"path":          "/repo",
			},
		},
		{
			FactKind: factKindFile,
			Payload: map[string]any{
				"repo_id":       "repo-a",
				"relative_path": "../outside.py",
				"parsed_file_data": map[string]any{
					"path": "../outside.py",
				},
			},
		},
	})

	if _, ok := result.scopesByRepoID["repo-a"]; ok {
		t.Fatalf("unsafe full-refresh file ownership produced scope: %#v", result.scopesByRepoID["repo-a"])
	}
	if got, want := result.fullRefreshFallbackRepos, 1; got != want {
		t.Fatalf("fullRefreshFallbackRepos = %d, want %d", got, want)
	}
}

func TestBuildCodeCallFileScopesFallsBackWhenFullRefreshExceedsSafetyCap(t *testing.T) {
	t.Parallel()

	envelopes := make([]facts.Envelope, 0, 3)
	envelopes = append(envelopes, facts.Envelope{
		FactKind: factKindRepository,
		Payload: map[string]any{
			"repo_id":       "repo-a",
			"source_run_id": "run-a",
			"path":          "/repo",
		},
	})
	for _, relativePath := range []string{"a.py", "b.py"} {
		envelopes = append(envelopes, facts.Envelope{
			FactKind: factKindFile,
			Payload: map[string]any{
				"repo_id":       "repo-a",
				"relative_path": relativePath,
				"parsed_file_data": map[string]any{
					"path": relativePath,
				},
			},
		})
	}

	scopesByRepoID, fallbackRepos := buildCodeCallFullRefreshFileScopesByRepoIDWithLimit(envelopes, nil, 1)
	if _, ok := scopesByRepoID["repo-a"]; ok {
		t.Fatalf("over-cap full-refresh file ownership produced scope: %#v", scopesByRepoID["repo-a"])
	}
	if got, want := fallbackRepos, 1; got != want {
		t.Fatalf("fallbackRepos = %d, want %d", got, want)
	}
}

func TestBuildCodeCallFileScopesFallsBackForConflictingFullRefreshRoots(t *testing.T) {
	t.Parallel()

	result := buildCodeCallFileScopesByRepoID([]facts.Envelope{
		{
			FactKind: factKindRepository,
			Payload: map[string]any{
				"repo_id":       "repo-a",
				"source_run_id": "run-a",
				"path":          "/repo-a",
			},
		},
		{
			FactKind: factKindRepository,
			Payload: map[string]any{
				"repo_id":       "repo-a",
				"source_run_id": "run-a",
				"path":          "/other-repo-a",
			},
		},
		{
			FactKind: factKindFile,
			Payload: map[string]any{
				"repo_id":       "repo-a",
				"relative_path": "caller.py",
				"parsed_file_data": map[string]any{
					"path": "caller.py",
				},
			},
		},
	})

	if _, ok := result.scopesByRepoID["repo-a"]; ok {
		t.Fatalf("conflicting roots produced full-refresh file scope: %#v", result.scopesByRepoID["repo-a"])
	}
	if got, want := result.fullRefreshFallbackRepos, 1; got != want {
		t.Fatalf("fullRefreshFallbackRepos = %d, want %d", got, want)
	}
}
