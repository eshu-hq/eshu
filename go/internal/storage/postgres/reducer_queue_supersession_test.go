// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestReducerQueueClaimSupersedesInactiveGenerationReducerWork(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 3, 14, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: nil},
		},
	}
	queue := ReducerQueue{
		db:            db,
		LeaseOwner:    "test-owner",
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return now },
	}

	_, claimed, err := queue.Claim(context.Background())
	if err != nil {
		t.Fatalf("Claim() error = %v", err)
	}
	if claimed {
		t.Fatal("Claim() claimed = true, want false from empty rows")
	}

	query := db.queries[0].query
	for _, want := range []string{
		"superseded_stale_reducer_generations AS (",
		"UPDATE fact_work_items AS stale",
		"status = 'superseded'",
		"failure_class = 'reducer_superseded_by_newer_active_generation'",
		"failure_message = 'reducer work superseded by newer active generation'",
		"stale.status IN ('pending', 'retrying', 'failed', 'dead_letter')",
		"scope.active_generation_id = active_generation.generation_id",
		"stale_generation.ingested_at < active_generation.ingested_at",
		"stale_generation.generation_id < active_generation.generation_id",
		"AND ($2::text[] IS NULL OR stale.domain = ANY($2::text[]))",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("claim query missing inactive-generation supersession %q:\n%s", want, query)
		}
	}
	if strings.Contains(query, "stale.status IN ('pending', 'retrying', 'claimed', 'running'") {
		t.Fatalf("claim query must not silently supersede live claimed/running reducer work:\n%s", query)
	}
}

func TestReducerQueueBatchClaimSupersedesInactiveGenerationReducerWork(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 6, 15, 30, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: nil},
		},
	}
	queue := ReducerQueue{
		db:            db,
		LeaseOwner:    "test-owner",
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return now },
	}

	intents, err := queue.ClaimBatch(context.Background(), 8)
	if err != nil {
		t.Fatalf("ClaimBatch() error = %v", err)
	}
	if len(intents) != 0 {
		t.Fatalf("ClaimBatch() returned %d intents, want 0 from empty rows", len(intents))
	}

	query := db.queries[0].query
	for _, want := range []string{
		"superseded_stale_reducer_generations AS (",
		"UPDATE fact_work_items AS stale",
		"status = 'superseded'",
		"failure_class = 'reducer_superseded_by_newer_active_generation'",
		"failure_message = 'reducer work superseded by newer active generation'",
		"stale.status IN ('pending', 'retrying', 'failed', 'dead_letter')",
		"scope.active_generation_id = active_generation.generation_id",
		"stale_generation.ingested_at < active_generation.ingested_at",
		"stale_generation.generation_id < active_generation.generation_id",
		"AND ($2::text[] IS NULL OR stale.domain = ANY($2::text[]))",
		"FROM superseded_stale_reducer_generations AS superseded",
		"superseded.work_item_id = fact_work_items.work_item_id",
		"FROM superseded_stale_reducer_generations AS superseded_same",
		"superseded_same.work_item_id = same.work_item_id",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("batch claim query missing inactive-generation supersession %q:\n%s", want, query)
		}
	}
	if strings.Contains(query, "stale.status IN ('pending', 'retrying', 'claimed', 'running'") {
		t.Fatalf("batch claim query must not silently supersede live claimed/running reducer work:\n%s", query)
	}
}
