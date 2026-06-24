// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

func TestSemanticExtractionQueueStoreObservabilitySnapshotAggregatesRedactedRows(t *testing.T) {
	t.Parallel()

	updatedAt := semanticQueueStorageTime().Add(2 * time.Minute)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{{
			rows: [][]any{
				{
					"pending", "documentation", "deepseek", "semantic-docs-default", "hosted",
					"allowed", "allowed", "allowed", "allowed", "hosted_worker", "acl_allowed",
					"", "allowed", "allowed", "daily_tokens", int64(2),
					int64(200), int64(60), int64(300), int64(0), int64(0), int64(0), int64(800), int64(700),
					updatedAt,
				},
				{
					"skipped_budget", "documentation", "deepseek", "semantic-docs-default", "hosted",
					"allowed", "allowed", "allowed", "allowed", "hosted_worker", "acl_allowed",
					"", "exhausted", "daily_limit", "daily_tokens", int64(1),
					int64(100), int64(30), int64(150), int64(0), int64(0), int64(0), int64(0), int64(0),
					updatedAt.Add(time.Minute),
				},
				{
					"dead_letter", "code_hints", "deepseek", "semantic-code-default", "hosted",
					"allowed", "allowed", "allowed", "allowed", "hosted_worker", "acl_allowed",
					"provider_unavailable", "allowed", "allowed", "daily_tokens", int64(1),
					int64(80), int64(20), int64(120), int64(75), int64(19), int64(115), int64(700), int64(600),
					updatedAt.Add(2 * time.Minute),
				},
			},
		}},
	}
	store := NewSemanticExtractionQueueStore(db)

	snapshot, err := store.ObservabilitySnapshot(context.Background())
	if err != nil {
		t.Fatalf("ObservabilitySnapshot() error = %v, want nil", err)
	}

	if got, want := snapshot.Queue.Total, 4; got != want {
		t.Fatalf("Queue.Total = %d, want %d", got, want)
	}
	if got, want := snapshot.Queue.Pending, 2; got != want {
		t.Fatalf("Queue.Pending = %d, want %d", got, want)
	}
	if got, want := snapshot.Queue.DeadLetter, 1; got != want {
		t.Fatalf("Queue.DeadLetter = %d, want %d", got, want)
	}
	if got, want := snapshot.Queue.BudgetExhausted, 1; got != want {
		t.Fatalf("Queue.BudgetExhausted = %d, want %d", got, want)
	}
	if got, want := snapshot.Budget.EstimatedInputTokens, int64(380); got != want {
		t.Fatalf("Budget.EstimatedInputTokens = %d, want %d", got, want)
	}
	if got, want := snapshot.Budget.ActualCostMicros, int64(115); got != want {
		t.Fatalf("Budget.ActualCostMicros = %d, want %d", got, want)
	}
	if got, want := snapshot.Budget.Exhausted, 1; got != want {
		t.Fatalf("Budget.Exhausted = %d, want %d", got, want)
	}
	if got, want := namedCountValue(snapshot.Queue.SourceClassCounts, "documentation"), 3; got != want {
		t.Fatalf("source_class documentation = %d, want %d", got, want)
	}
	if got, want := snapshot.Queue.ProviderProfileCounts[0].ProviderProfileID, "semantic-docs-default"; got != want {
		t.Fatalf("ProviderProfileCounts[0].ProviderProfileID = %q, want %q", got, want)
	}
	if got, want := namedCountValue(snapshot.Queue.FailureClassCounts, "provider_unavailable"), 1; got != want {
		t.Fatalf("failure_class provider_unavailable = %d, want %d", got, want)
	}
	if got, want := namedCountValue(snapshot.Audit.ActorClassCounts, "hosted_worker"), 4; got != want {
		t.Fatalf("audit actor hosted_worker = %d, want %d", got, want)
	}
	if snapshot.Audit.LastProcessedAt.IsZero() {
		t.Fatal("Audit.LastProcessedAt is zero, want latest updated_at")
	}

	query := db.queries[0].query
	for _, forbidden := range []string{
		"source_id_hash",
		"chunk_id_hash",
		"fingerprint",
		"failure_message",
		"failure_details",
		"response_hash",
		"prompt_text",
		"response_text",
	} {
		if strings.Contains(query, forbidden) {
			t.Fatalf("ObservabilitySnapshot query exposes %q:\n%s", forbidden, query)
		}
	}
	for _, want := range []string{
		"budget_metadata->>'estimated_input_tokens'",
		"budget_metadata->>'EstimatedInputTokens'",
		"GROUP BY",
		"provider_profile_class",
		"policy_state",
		"guard_state",
		"failure_class",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("ObservabilitySnapshot query missing %q:\n%s", want, query)
		}
	}
}

func namedCountValue(rows []statuspkg.NamedCount, name string) int {
	for _, row := range rows {
		if row.Name == name {
			return row.Count
		}
	}
	return 0
}
