// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package rationale

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
)

func TestRationaleSameFileEdgesKeepDistinctPartitionAndIntentIDs(t *testing.T) {
	t.Parallel()
	const repoID = "repo-rationale-identity"
	contextByRepoID := map[string]sharedintent.ProjectionContext{
		repoID: {
			ScopeID: "scope-rationale-identity", SourceRunID: "run-rationale-identity",
			GenerationID: "gen-rationale-identity",
		},
	}
	edges := []map[string]any{
		{
			"repo_id": repoID, "target_path": "src/same.go",
			"rationale_uid": "rationale:one", "target_entity_id": "content-entity:one",
		},
		{
			"repo_id": repoID, "target_path": "src/same.go",
			"rationale_uid": "rationale:two", "target_entity_id": "content-entity:two",
		},
	}
	rows := BuildSharedIntentRows(
		edges, DeltaScope{}, []string{repoID}, contextByRepoID,
		time.Date(2026, time.August, 15, 0, 0, 0, 0, time.UTC),
	)

	var edgeRows []sharedintent.Row
	for _, row := range rows {
		if !isRepoRefreshRow(row) {
			edgeRows = append(edgeRows, row)
		}
	}
	if len(edgeRows) != 2 {
		t.Fatalf("same-file edge intents = %d, want 2", len(edgeRows))
	}
	if edgeRows[0].PartitionKey == edgeRows[1].PartitionKey {
		t.Errorf("same-file edges share partition key %q; edge identity must keep them distinct", edgeRows[0].PartitionKey)
	}
	if edgeRows[0].IntentID == edgeRows[1].IntentID {
		t.Errorf("same-file edges share intent id %q; both edges would collapse", edgeRows[0].IntentID)
	}
}

func TestRationaleIntentCommentsDoNotClaimSameFilePartitionSharing(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("intents.go")
	if err != nil {
		t.Fatalf("read intents.go: %v", err)
	}
	for _, stale := range []string{
		"many edges in one file share a partition key",
		"many edges share one file-scoped partition key",
	} {
		if strings.Contains(string(raw), stale) {
			t.Errorf("intents.go retains false partition-sharing claim %q", stale)
		}
	}
}
