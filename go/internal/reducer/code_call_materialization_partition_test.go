// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestBuildCodeCallRefreshIntentsUseVersionedDeltaPartitionKey(t *testing.T) {
	t.Parallel()

	createdAt := time.Date(2026, time.June, 14, 10, 0, 0, 0, time.UTC)
	contextByRepoID := map[string]ProjectionContext{
		"repo-a": {
			ScopeID:          "scope-a",
			AcceptanceUnitID: "repository:repo-a",
			SourceRunID:      "run-a",
			GenerationID:     "gen-a",
		},
	}
	deltaFileScopesByRepoID := map[string]codeCallDeltaFileScope{
		"repo-a": {
			filePaths:      []string{"/repo/src/b.go", "/repo/src/a.go", "/repo/src/a.go"},
			partitionPaths: []string{"src/b.go", "src/a.go", "src/a.go"},
		},
	}

	first := buildCodeCallRefreshIntentsWithDeltaFileScopes(contextByRepoID, deltaFileScopesByRepoID, createdAt)
	second := buildCodeCallRefreshIntentsWithDeltaFileScopes(contextByRepoID, deltaFileScopesByRepoID, createdAt)
	if got, want := len(first), 1; got != want {
		t.Fatalf("len(first) = %d, want %d", got, want)
	}
	if got, want := len(second), 1; got != want {
		t.Fatalf("len(second) = %d, want %d", got, want)
	}

	row := first[0]
	if !strings.HasPrefix(row.PartitionKey, "code-calls:v1:files:repo-a:") {
		t.Fatalf("PartitionKey = %q, want versioned file partition key", row.PartitionKey)
	}
	if strings.Contains(row.PartitionKey, "/repo/") || strings.Contains(row.PartitionKey, "src/") {
		t.Fatalf("PartitionKey = %q leaks raw affected paths", row.PartitionKey)
	}
	if row.IntentID != second[0].IntentID {
		t.Fatalf("duplicate replay IntentID = %q, want %q", second[0].IntentID, row.IntentID)
	}
	gotPaths, ok := row.Payload["delta_file_paths"].([]string)
	if !ok {
		t.Fatalf("delta_file_paths type = %T, want []string", row.Payload["delta_file_paths"])
	}
	if got, want := len(gotPaths), 2; got != want {
		t.Fatalf("delta_file_paths = %#v, want all changed files in one refresh row", gotPaths)
	}

	key, ok := row.AcceptanceKey()
	if !ok {
		t.Fatal("AcceptanceKey() ok = false, want true")
	}
	if got, want := key.AcceptanceUnitID, "repository:repo-a"; got != want {
		t.Fatalf("AcceptanceUnitID = %q, want %q", got, want)
	}

	wholeScope := buildCodeCallRefreshIntentsWithDeltaFileScopes(contextByRepoID, nil, createdAt)
	if got, want := wholeScope[0].PartitionKey, "code-calls:v1:whole:repo-a"; got != want {
		t.Fatalf("whole-scope PartitionKey = %q, want %q", got, want)
	}
	wholeKey, ok := wholeScope[0].AcceptanceKey()
	if !ok {
		t.Fatal("whole-scope AcceptanceKey() ok = false, want true")
	}
	if wholeKey != key {
		t.Fatalf("whole-scope acceptance key = %#v, want %#v", wholeKey, key)
	}

	otherRoot := map[string]codeCallDeltaFileScope{
		"repo-a": {
			filePaths:      []string{"/other/src/b.go", "/other/src/a.go"},
			partitionPaths: []string{"src/b.go", "src/a.go"},
		},
	}
	otherRootRows := buildCodeCallRefreshIntentsWithDeltaFileScopes(contextByRepoID, otherRoot, createdAt)
	if got, want := otherRootRows[0].PartitionKey, row.PartitionKey; got != want {
		t.Fatalf("PartitionKey changed with absolute root: got %q, want %q", got, want)
	}
}

func TestCodeCallRefreshPartitionKeyFallsBackForUnsafeAffectedFiles(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		filePaths []string
	}{
		{name: "missing", filePaths: nil},
		{name: "empty", filePaths: []string{""}},
		{name: "absolute", filePaths: []string{"/repo/src/changed.go"}},
		{name: "parent", filePaths: []string{"../changed.go"}},
		{name: "mixed malformed", filePaths: []string{"src/changed.go", ""}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := codeCallRefreshPartitionKeyForDelta("repo-a", tt.filePaths)
			if want := "code-calls:v1:whole:repo-a"; got != want {
				t.Fatalf("partition key = %q, want whole-scope fallback %q", got, want)
			}
		})
	}
}

func TestBuildCodeCallDeltaFileScopesRejectsUnsafeAffectedPath(t *testing.T) {
	t.Parallel()

	got := buildCodeCallDeltaFileScopesByRepoID([]facts.Envelope{
		{
			FactKind: factKindRepository,
			Payload: map[string]any{
				"repo_id":                      "repo-a",
				"path":                         "/repo",
				"delta_generation":             true,
				"delta_relative_paths":         []string{"src/changed.go"},
				"delta_deleted_relative_paths": []string{"../outside.go"},
			},
		},
	})
	if _, ok := got["repo-a"]; ok {
		t.Fatalf("unsafe repo delta produced file scope: %#v", got["repo-a"])
	}
}
