// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestSharedIntentStoreHasCompletedAcceptanceUnitSourceRunPartitionDomainIntents(t *testing.T) {
	t.Parallel()

	db := &partitionHistoryTestDB{completed: map[string]bool{"partition-a": true}}
	store := NewSharedIntentStore(db)
	ctx := context.Background()

	got, err := store.HasCompletedAcceptanceUnitSourceRunPartitionDomainIntents(ctx, reducer.SharedProjectionAcceptanceKey{
		ScopeID:          "scope-a",
		AcceptanceUnitID: "repo-a",
		SourceRunID:      "run-new",
	}, "partition-a", reducer.DomainCodeCalls)
	if err != nil {
		t.Fatalf("HasCompletedAcceptanceUnitSourceRunPartitionDomainIntents: %v", err)
	}
	if !got {
		t.Fatal("HasCompletedAcceptanceUnitSourceRunPartitionDomainIntents = false, want true")
	}

	got, err = store.HasCompletedAcceptanceUnitSourceRunPartitionDomainIntents(ctx, reducer.SharedProjectionAcceptanceKey{
		ScopeID:          "scope-a",
		AcceptanceUnitID: "repo-a",
		SourceRunID:      "run-new",
	}, "partition-b", reducer.DomainCodeCalls)
	if err != nil {
		t.Fatalf("HasCompletedAcceptanceUnitSourceRunPartitionDomainIntents other partition: %v", err)
	}
	if got {
		t.Fatal("HasCompletedAcceptanceUnitSourceRunPartitionDomainIntents for pending partition = true, want false")
	}
	if got, want := db.queryArgs, []any{
		"scope-a",
		"repo-a",
		"run-new",
		"partition-b",
		reducer.DomainCodeCalls,
	}; !equalPartitionHistoryArgs(got, want) {
		t.Fatalf("query args = %#v, want %#v", got, want)
	}
	if !strings.Contains(db.query, "partition_key = $4") {
		t.Fatalf("query = %q, want partition scoped lookup", db.query)
	}
}

func TestSharedIntentStoreHasCompletedAcceptanceUnitSourceRunRefreshDomainIntents(t *testing.T) {
	t.Parallel()

	db := &partitionHistoryTestDB{refreshCompleted: true}
	store := NewSharedIntentStore(db)
	ctx := context.Background()

	got, err := store.HasCompletedAcceptanceUnitSourceRunRefreshDomainIntents(ctx, reducer.SharedProjectionAcceptanceKey{
		ScopeID:          "scope-a",
		AcceptanceUnitID: "repo-a",
		SourceRunID:      "run-new",
	}, []string{"/repo/src/models.go"}, reducer.DomainCodeCalls)
	if err != nil {
		t.Fatalf("HasCompletedAcceptanceUnitSourceRunRefreshDomainIntents: %v", err)
	}
	if !got {
		t.Fatal("HasCompletedAcceptanceUnitSourceRunRefreshDomainIntents = false, want true")
	}
	if got, want := len(db.queryArgs), 5; got != want {
		t.Fatalf("query arg count = %d, want %d", got, want)
	}
	if !strings.Contains(db.query, "payload->>'intent_type' = 'repo_refresh'") {
		t.Fatalf("query = %q, want repo_refresh filter", db.query)
	}
	if !strings.Contains(db.query, "jsonb_array_elements_text(payload->'delta_file_paths')") {
		t.Fatalf("query = %q, want delta_file_paths coverage check", db.query)
	}

	got, err = store.HasCompletedAcceptanceUnitSourceRunRefreshDomainIntents(ctx, reducer.SharedProjectionAcceptanceKey{
		ScopeID:          "scope-a",
		AcceptanceUnitID: "repo-a",
		SourceRunID:      "run-new",
	}, nil, reducer.DomainCodeCalls)
	if err != nil {
		t.Fatalf("HasCompletedAcceptanceUnitSourceRunRefreshDomainIntents empty files: %v", err)
	}
	if got {
		t.Fatal("HasCompletedAcceptanceUnitSourceRunRefreshDomainIntents empty files = true, want false")
	}
}

func TestSharedIntentStoreCodeCallProjectionRowBlockedByRepoFence(t *testing.T) {
	t.Parallel()

	db := &partitionHistoryTestDB{fenceSelectedExists: true, fenceBlocked: true}
	store := NewSharedIntentStore(db)
	ctx := context.Background()
	row := reducer.SharedProjectionIntentRow{
		IntentID:         "caller-edge",
		PartitionKey:     reducer.CodeCallProjectionFilePartitionKeyPrefix() + "abc123",
		RepositoryID:     "repo-a",
		CreatedAt:        time.Date(2026, time.June, 19, 9, 30, 0, 0, time.UTC),
		ProjectionDomain: reducer.DomainCodeCalls,
		Payload: map[string]any{
			"delta_file_paths": []string{"/repo/src/caller.go"},
		},
	}

	blocked, err := store.CodeCallProjectionRowBlockedByRepoFence(ctx, reducer.SharedProjectionAcceptanceKey{
		ScopeID:          "scope-a",
		AcceptanceUnitID: "repo-a",
		SourceRunID:      "run-1",
	}, row, reducer.DomainCodeCalls)
	if err != nil {
		t.Fatalf("CodeCallProjectionRowBlockedByRepoFence: %v", err)
	}
	if !blocked {
		t.Fatal("CodeCallProjectionRowBlockedByRepoFence = false, want true")
	}
	if !strings.Contains(db.query, "WITH selected AS") {
		t.Fatalf("query = %q, want selected-row existence guard", db.query)
	}
	if strings.Contains(db.query, "SELECT intent_id") {
		t.Fatalf("query = %q, want EXISTS lookup without loading intent rows", db.query)
	}
	for _, want := range []string{
		"candidate.completed_at IS NULL",
		"candidate.repository_id = $6",
		"candidate.partition_key = $8::text",
		"candidate.partition_key LIKE $9",
		"jsonb_array_elements_text(",
		"CASE",
		"ELSE '[]'::jsonb",
	} {
		if !strings.Contains(db.query, want) {
			t.Fatalf("query missing %q:\n%s", want, db.query)
		}
	}
	if strings.Contains(db.query, "$13") {
		t.Fatalf("query = %q, want no unused high-numbered placeholder", db.query)
	}
	if got, want := db.queryArgs[0], "caller-edge"; got != want {
		t.Fatalf("intent arg = %#v, want %#v", got, want)
	}
	if got, want := db.queryArgs[5], "repo-a"; got != want {
		t.Fatalf("repository arg = %#v, want %#v", got, want)
	}
	if got, want := len(db.queryArgs), 12; got != want {
		t.Fatalf("query arg count = %d, want %d", got, want)
	}
	if got, want := db.queryArgs[7], row.PartitionKey; got != want {
		t.Fatalf("partition key arg = %#v, want %#v", got, want)
	}
	if got, want := db.queryArgs[8], reducer.CodeCallProjectionFilePartitionKeyPrefix()+"%"; got != want {
		t.Fatalf("file prefix arg = %#v, want %#v", got, want)
	}
	if got, want := db.queryArgs[9], true; got != want {
		t.Fatalf("rowCanBeCoveredByFileRefresh arg = %#v, want %#v", got, want)
	}
	if got, want := db.queryArgs[10], true; got != want {
		t.Fatalf("rowCanBeCoveredByFileRefreshByPath arg = %#v, want %#v", got, want)
	}
}

func TestSharedIntentStoreCodeCallProjectionRowBlockedByRepoFenceDoesNotFenceFileRefreshRows(t *testing.T) {
	t.Parallel()

	db := &partitionHistoryTestDB{fenceSelectedExists: true, fenceBlocked: false}
	store := NewSharedIntentStore(db)
	row := reducer.SharedProjectionIntentRow{
		IntentID:     "refresh-edge",
		PartitionKey: reducer.CodeCallProjectionFilePartitionKeyPrefix() + "abc123",
		RepositoryID: "repo-a",
		CreatedAt:    time.Date(2026, time.June, 19, 9, 35, 0, 0, time.UTC),
		Payload: map[string]any{
			"intent_type":      "repo_refresh",
			"delta_file_paths": []any{"/repo/src/caller.go"},
		},
	}

	blocked, err := store.CodeCallProjectionRowBlockedByRepoFence(context.Background(), reducer.SharedProjectionAcceptanceKey{
		ScopeID:          "scope-a",
		AcceptanceUnitID: "repo-a",
		SourceRunID:      "run-1",
	}, row, reducer.DomainCodeCalls)
	if err != nil {
		t.Fatalf("CodeCallProjectionRowBlockedByRepoFence: %v", err)
	}
	if blocked {
		t.Fatal("CodeCallProjectionRowBlockedByRepoFence = true, want false")
	}
	if got, want := db.queryArgs[9], false; got != want {
		t.Fatalf("rowCanBeCoveredByFileRefresh arg = %#v, want %#v", got, want)
	}
	if got, want := db.queryArgs[10], false; got != want {
		t.Fatalf("rowCanBeCoveredByFileRefreshByPath arg = %#v, want %#v", got, want)
	}
}

func TestSharedIntentStoreCodeCallProjectionRowBlockedByRepoFenceUsesWholeRowLookup(t *testing.T) {
	t.Parallel()

	db := &partitionHistoryTestDB{fenceSelectedExists: true, fenceBlocked: true}
	store := NewSharedIntentStore(db)
	row := reducer.SharedProjectionIntentRow{
		IntentID:         "whole-edge",
		PartitionKey:     "v1:whole:repo-a",
		RepositoryID:     "repo-a",
		CreatedAt:        time.Date(2026, time.June, 19, 10, 0, 0, 0, time.UTC),
		ProjectionDomain: reducer.DomainCodeCalls,
	}

	blocked, err := store.CodeCallProjectionRowBlockedByRepoFence(context.Background(), reducer.SharedProjectionAcceptanceKey{
		ScopeID:          "scope-a",
		AcceptanceUnitID: "repo-a",
		SourceRunID:      "run-1",
	}, row, reducer.DomainCodeCalls)
	if err != nil {
		t.Fatalf("CodeCallProjectionRowBlockedByRepoFence: %v", err)
	}
	if !blocked {
		t.Fatal("CodeCallProjectionRowBlockedByRepoFence = false, want true")
	}
	for _, want := range []string{
		"candidate.completed_at IS NULL",
		"candidate.repository_id = $6",
		"(candidate.created_at, candidate.intent_id) < ($7, $1)",
	} {
		if !strings.Contains(db.query, want) {
			t.Fatalf("whole-row query missing %q:\n%s", want, db.query)
		}
	}
	for _, reject := range []string{
		"jsonb_array_elements_text(",
		"candidate.partition_key LIKE",
		"payload->>'intent_type' = 'repo_refresh'",
	} {
		if strings.Contains(db.query, reject) {
			t.Fatalf("whole-row query contains %q:\n%s", reject, db.query)
		}
	}
	if got, want := len(db.queryArgs), 7; got != want {
		t.Fatalf("whole-row query arg count = %d, want %d", got, want)
	}
}

func TestSharedIntentStoreCodeCallProjectionRowBlockedByRepoFenceUsesPayloadRepoFallback(t *testing.T) {
	t.Parallel()

	db := &partitionHistoryTestDB{fenceSelectedExists: true, fenceBlocked: true}
	store := NewSharedIntentStore(db)
	row := reducer.SharedProjectionIntentRow{
		IntentID:     "payload-repo-edge",
		PartitionKey: "v1:whole:repo-a",
		CreatedAt:    time.Date(2026, time.June, 19, 10, 15, 0, 0, time.UTC),
		Payload: map[string]any{
			"repo_id": "repo-a",
		},
	}

	blocked, err := store.CodeCallProjectionRowBlockedByRepoFence(context.Background(), reducer.SharedProjectionAcceptanceKey{
		ScopeID:          "scope-a",
		AcceptanceUnitID: "repo-a",
		SourceRunID:      "run-1",
	}, row, reducer.DomainCodeCalls)
	if err != nil {
		t.Fatalf("CodeCallProjectionRowBlockedByRepoFence: %v", err)
	}
	if !blocked {
		t.Fatal("CodeCallProjectionRowBlockedByRepoFence = false, want true")
	}
	if got, want := db.queryArgs[5], "repo-a"; got != want {
		t.Fatalf("repository arg = %#v, want %#v", got, want)
	}
}

func TestSharedIntentStoreCodeCallProjectionRowBlockedByRepoFenceTreatsMissingSelectedRowAsBlocked(t *testing.T) {
	t.Parallel()

	db := &partitionHistoryTestDB{fenceSelectedExists: false, fenceBlocked: false}
	store := NewSharedIntentStore(db)
	blocked, err := store.CodeCallProjectionRowBlockedByRepoFence(context.Background(), reducer.SharedProjectionAcceptanceKey{
		ScopeID:          "scope-a",
		AcceptanceUnitID: "repo-a",
		SourceRunID:      "run-1",
	}, reducer.SharedProjectionIntentRow{
		IntentID:     "caller-edge",
		PartitionKey: reducer.CodeCallProjectionFilePartitionKeyPrefix() + "abc123",
		RepositoryID: "repo-a",
		CreatedAt:    time.Date(2026, time.June, 19, 9, 45, 0, 0, time.UTC),
	}, reducer.DomainCodeCalls)
	if err != nil {
		t.Fatalf("CodeCallProjectionRowBlockedByRepoFence: %v", err)
	}
	if !blocked {
		t.Fatal("CodeCallProjectionRowBlockedByRepoFence = false, want missing selected row to block")
	}
}

type partitionHistoryTestDB struct {
	completed           map[string]bool
	generationReady     bool
	refreshCompleted    bool
	fenceSelectedExists bool
	fenceBlocked        bool
	query               string
	queryArgs           []any
}

func (db *partitionHistoryTestDB) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, fmt.Errorf("unexpected exec")
}

func (db *partitionHistoryTestDB) QueryContext(_ context.Context, query string, args ...any) (Rows, error) {
	db.query = query
	db.queryArgs = append([]any(nil), args...)
	if strings.Contains(query, "blocked_by_fence") {
		return &partitionHistoryRows{values: []bool{db.fenceSelectedExists, db.fenceBlocked}, idx: -1}, nil
	}
	if strings.Contains(query, "payload->>'intent_type' = 'repo_refresh'") {
		return &partitionHistoryRows{values: []bool{db.refreshCompleted}, idx: -1}, nil
	}
	if strings.Contains(query, "generation_id = $4") {
		return &partitionHistoryRows{values: []bool{db.generationReady}, idx: -1}, nil
	}
	partitionKey := args[3].(string)
	return &partitionHistoryRows{values: []bool{db.completed[partitionKey]}, idx: -1}, nil
}

type partitionHistoryRows struct {
	values []bool
	idx    int
}

func (r *partitionHistoryRows) Next() bool {
	r.idx++
	return r.idx == 0
}

func (r *partitionHistoryRows) Scan(dest ...any) error {
	if len(dest) != len(r.values) {
		return fmt.Errorf("scan destinations = %d, want %d", len(dest), len(r.values))
	}
	for i := range dest {
		exists, ok := dest[i].(*bool)
		if !ok {
			return fmt.Errorf("scan destination type = %T, want *bool", dest[i])
		}
		*exists = r.values[i]
	}
	return nil
}

func (r *partitionHistoryRows) Err() error {
	return nil
}

func (r *partitionHistoryRows) Close() error {
	return nil
}

func equalPartitionHistoryArgs(got []any, want []any) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// TestSharedIntentStoreCodeCallWholeFenceRanksRefreshFirst guards the #3865 fix:
// the whole-fence query must rank candidates by is_refresh_intent first, so a
// repo-refresh intent is never fenced behind its own (older) edges. A revert to a
// raw (created_at, intent_id) comparison would reintroduce the deadlock.
func TestSharedIntentStoreCodeCallWholeFenceRanksRefreshFirst(t *testing.T) {
	t.Parallel()

	db := &partitionHistoryTestDB{fenceSelectedExists: true, fenceBlocked: false}
	store := NewSharedIntentStore(db)

	_, err := store.CodeCallProjectionRowBlockedByRepoFence(
		context.Background(),
		reducer.SharedProjectionAcceptanceKey{ScopeID: "scope-a", AcceptanceUnitID: "repo-a", SourceRunID: "run-1"},
		reducer.SharedProjectionIntentRow{
			IntentID:         "whole-1",
			ProjectionDomain: reducer.DomainCodeCalls,
			PartitionKey:     "code-calls:v1:whole:repo-a",
			ScopeID:          "scope-a",
			AcceptanceUnitID: "repo-a",
			RepositoryID:     "repo-a",
			SourceRunID:      "run-1",
			Payload:          map[string]any{"action": "refresh", "repo_id": "repo-a"},
		},
		reducer.DomainCodeCalls,
	)
	if err != nil {
		t.Fatalf("CodeCallProjectionRowBlockedByRepoFence: %v", err)
	}
	if !strings.Contains(db.query, "blocked_by_fence") {
		t.Fatalf("expected the whole-fence query, got %q", db.query)
	}
	// The refresh-priority guard: a refresh candidate precedes a non-refresh
	// selected row, and same-class rows fall back to (created_at, intent_id).
	if !strings.Contains(db.query, "candidate.is_refresh_intent AND NOT selected.is_refresh_intent") {
		t.Fatalf("whole-fence must exempt a refresh row from non-refresh edges (#3865); got %q", db.query)
	}
	if !strings.Contains(db.query, "candidate.is_refresh_intent = selected.is_refresh_intent") {
		t.Fatalf("whole-fence must tie-break within an is_refresh_intent class; got %q", db.query)
	}
}

// TestSharedIntentStoreCodeCallFileFenceRanksRefreshFirst guards the file-lane
// twin of the #3865 fix: the file-fence's non-file branch must also rank by
// is_refresh_intent so a file repo_refresh is never fenced behind an older
// non-file edge.
func TestSharedIntentStoreCodeCallFileFenceRanksRefreshFirst(t *testing.T) {
	t.Parallel()

	db := &partitionHistoryTestDB{fenceSelectedExists: true, fenceBlocked: false}
	store := NewSharedIntentStore(db)

	_, err := store.CodeCallProjectionRowBlockedByRepoFence(
		context.Background(),
		reducer.SharedProjectionAcceptanceKey{ScopeID: "scope-a", AcceptanceUnitID: "repo-a", SourceRunID: "run-1"},
		reducer.SharedProjectionIntentRow{
			IntentID:         "file-refresh-1",
			ProjectionDomain: reducer.DomainCodeCalls,
			PartitionKey:     "code-calls:v1:files:repo-a:src/a.go",
			ScopeID:          "scope-a",
			AcceptanceUnitID: "repo-a",
			RepositoryID:     "repo-a",
			SourceRunID:      "run-1",
			Payload: map[string]any{
				"action": "refresh", "intent_type": "repo_refresh", "repo_id": "repo-a",
				"delta_projection": true, "delta_file_paths": []string{"src/a.go"},
			},
		},
		reducer.DomainCodeCalls,
	)
	if err != nil {
		t.Fatalf("CodeCallProjectionRowBlockedByRepoFence: %v", err)
	}
	if !strings.Contains(db.query, "candidate.is_refresh_intent AND NOT selected.is_refresh_intent") {
		t.Fatalf("file-fence non-file branch must rank refresh-first (#3865 file lane); got %q", db.query)
	}
}
