// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status_test

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/status"
)

func TestSemanticExtractionObservabilitySurvivesMissingCapabilityState(t *testing.T) {
	t.Parallel()

	report := status.BuildReport(status.RawSnapshot{
		SemanticExtraction: status.SemanticExtractionStatus{
			Queue: status.SemanticExtractionQueueSnapshot{
				Total:   2,
				Pending: 2,
			},
			Budget: status.SemanticExtractionBudgetSnapshot{
				EstimatedInputTokens: 120,
			},
			Audit: status.SemanticExtractionAuditSnapshot{
				ActorClassCounts: []status.NamedCount{{Name: "hosted_worker", Count: 2}},
			},
		},
	}, status.DefaultOptions())

	if got, want := report.SemanticExtraction.State, status.SemanticExtractionUnavailable; got != want {
		t.Fatalf("SemanticExtraction.State = %q, want %q", got, want)
	}
	if report.SemanticExtraction.DeterministicPathsAffected {
		t.Fatal("DeterministicPathsAffected = true, want false")
	}
	if got, want := report.SemanticExtraction.Queue.Pending, 2; got != want {
		t.Fatalf("Queue.Pending = %d, want %d", got, want)
	}
	if got, want := report.SemanticExtraction.Budget.EstimatedInputTokens, int64(120); got != want {
		t.Fatalf("Budget.EstimatedInputTokens = %d, want %d", got, want)
	}
	if got, want := len(report.SemanticExtraction.Audit.ActorClassCounts), 1; got != want {
		t.Fatalf("Audit.ActorClassCounts len = %d, want %d", got, want)
	}
}
