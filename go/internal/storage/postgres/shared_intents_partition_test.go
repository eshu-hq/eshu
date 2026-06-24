// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestSharedIntentStoreListPendingAcceptanceUnitPartitionIntents(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC().Truncate(time.Microsecond)
	completedAt := now.Add(time.Second)
	db := &partitionIntentListTestDB{rows: []partitionIntentListRow{
		{
			row: reducer.SharedProjectionIntentRow{
				IntentID:         "si-other-partition",
				ProjectionDomain: reducer.DomainCodeCalls,
				PartitionKey:     "code-calls:v1:files:repo-a:other",
				RepositoryID:     "repo-a",
				SourceRunID:      "run-1",
				GenerationID:     "gen-1",
				Payload:          map[string]any{"repo_id": "repo-a"},
				CreatedAt:        now,
			},
			scopeID:          "scope-a",
			acceptanceUnitID: "repo-a",
		},
		{
			row: reducer.SharedProjectionIntentRow{
				IntentID:         "si-target-1",
				ProjectionDomain: reducer.DomainCodeCalls,
				PartitionKey:     "code-calls:v1:files:repo-a:target",
				RepositoryID:     "repo-a",
				SourceRunID:      "run-1",
				GenerationID:     "gen-1",
				Payload:          map[string]any{"repo_id": "repo-a"},
				CreatedAt:        now.Add(time.Millisecond),
			},
			scopeID:          "scope-a",
			acceptanceUnitID: "repo-a",
		},
		{
			row: reducer.SharedProjectionIntentRow{
				IntentID:         "si-target-completed",
				ProjectionDomain: reducer.DomainCodeCalls,
				PartitionKey:     "code-calls:v1:files:repo-a:target",
				RepositoryID:     "repo-a",
				SourceRunID:      "run-1",
				GenerationID:     "gen-1",
				Payload:          map[string]any{"repo_id": "repo-a"},
				CreatedAt:        now.Add(2 * time.Millisecond),
				CompletedAt:      &completedAt,
			},
			scopeID:          "scope-a",
			acceptanceUnitID: "repo-a",
		},
		{
			row: reducer.SharedProjectionIntentRow{
				IntentID:         "si-target-other-run",
				ProjectionDomain: reducer.DomainCodeCalls,
				PartitionKey:     "code-calls:v1:files:repo-a:target",
				RepositoryID:     "repo-a",
				SourceRunID:      "run-2",
				GenerationID:     "gen-2",
				Payload:          map[string]any{"repo_id": "repo-a"},
				CreatedAt:        now.Add(3 * time.Millisecond),
			},
			scopeID:          "scope-a",
			acceptanceUnitID: "repo-a",
		},
	}}
	store := NewSharedIntentStore(db)

	got, err := store.ListPendingAcceptanceUnitPartitionIntents(context.Background(), reducer.SharedProjectionAcceptanceKey{
		ScopeID:          "scope-a",
		AcceptanceUnitID: "repo-a",
		SourceRunID:      "run-1",
	}, reducer.DomainCodeCalls, "code-calls:v1:files:repo-a:target", 100)
	if err != nil {
		t.Fatalf("ListPendingAcceptanceUnitPartitionIntents: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1 target pending row", len(got))
	}
	if got[0].IntentID != "si-target-1" {
		t.Fatalf("IntentID = %q, want si-target-1", got[0].IntentID)
	}
	if !strings.Contains(db.query, "partition_key = $5") {
		t.Fatalf("query = %q, want partition key predicate", db.query)
	}
}

type partitionIntentListRow struct {
	row              reducer.SharedProjectionIntentRow
	scopeID          string
	acceptanceUnitID string
}

type partitionIntentListTestDB struct {
	rows  []partitionIntentListRow
	query string
}

func (db *partitionIntentListTestDB) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, fmt.Errorf("unexpected exec")
}

func (db *partitionIntentListTestDB) QueryContext(_ context.Context, query string, args ...any) (Rows, error) {
	db.query = query
	if !strings.Contains(query, "partition_key = $5") {
		return nil, fmt.Errorf("query missing partition key predicate: %s", query)
	}
	scopeID := args[0].(string)
	acceptanceUnitID := args[1].(string)
	runID := args[2].(string)
	domain := args[3].(string)
	partitionKey := args[4].(string)
	limit := args[5].(int)
	if limit < 1 {
		limit = 1
	}

	var rows [][]any
	for _, stored := range db.rows {
		intent := stored.row
		if stored.scopeID != scopeID || stored.acceptanceUnitID != acceptanceUnitID {
			continue
		}
		if intent.SourceRunID != runID || intent.ProjectionDomain != domain || intent.PartitionKey != partitionKey {
			continue
		}
		if intent.CompletedAt != nil {
			continue
		}
		payloadBytes, _ := json.Marshal(intent.Payload)
		rows = append(rows, []any{
			intent.IntentID,
			intent.ProjectionDomain,
			intent.PartitionKey,
			stored.scopeID,
			stored.acceptanceUnitID,
			intent.RepositoryID,
			intent.SourceRunID,
			intent.GenerationID,
			payloadBytes,
			intent.CreatedAt,
			nil,
		})
		if len(rows) >= limit {
			break
		}
	}
	return newSharedIntentRows(rows), nil
}
