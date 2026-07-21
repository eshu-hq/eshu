// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestSharedIntentStoreHasCompletedGenerationRefreshFenceHistory(t *testing.T) {
	t.Parallel()

	db := &partitionHistoryTestDB{generationReady: true}
	store := NewSharedIntentStore(db)
	ready, err := store.HasCompletedAcceptanceUnitSourceRunGenerationPartitionDomainIntents(
		context.Background(),
		reducer.SharedProjectionAcceptanceKey{
			ScopeID:          "scope-a",
			AcceptanceUnitID: "repo-a",
			SourceRunID:      "run-reused",
		},
		"gen-2",
		"refresh-key",
		reducer.DomainSQLRelationships,
	)
	if err != nil {
		t.Fatalf("HasCompletedAcceptanceUnitSourceRunGenerationPartitionDomainIntents: %v", err)
	}
	if !ready {
		t.Fatal("generation refresh fence = false, want true")
	}

	wantArgs := []any{
		"scope-a",
		"repo-a",
		"run-reused",
		"gen-2",
		"refresh-key",
		reducer.DomainSQLRelationships,
	}
	if !equalPartitionHistoryArgs(db.queryArgs, wantArgs) {
		t.Fatalf("query args = %#v, want %#v", db.queryArgs, wantArgs)
	}
	for _, want := range []string{
		"generation_id = $4",
		"partition_key = $5",
		"projection_domain = $6",
		"completed_at IS NOT NULL",
	} {
		if !strings.Contains(db.query, want) {
			t.Fatalf("query missing %q:\n%s", want, db.query)
		}
	}
	if strings.Contains(db.query, "completed_at IS NULL") {
		t.Fatalf("query contains pending-row anti-check that cannot identify a production redelivery:\n%s", db.query)
	}
}
