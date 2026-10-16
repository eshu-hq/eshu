// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package jira

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func freshnessSourceItem(generationID string) workflow.WorkItem {
	return workflow.WorkItem{
		CollectorKind:       scope.CollectorJira,
		CollectorInstanceID: "jira-primary",
		ScopeID:             "jira:site:example",
		GenerationID:        generationID,
		CurrentFencingToken: 11,
		CurrentClaimID:      "claim-1",
		WorkItemID:          "work-1",
		RunID:               "run-1",
		SourceSystem:        string(scope.CollectorJira),
		Status:              workflow.WorkItemStatusClaimed,
		CurrentOwnerID:      "owner-1",
	}
}

// TestClaimedSourceSetsDistinctHintsAcrossWindows proves the #7926 freshness
// fix at the source seam: the same issue content collected under a later
// window carries a different hint, so the store treats the later observation
// as new content instead of skipping it as unchanged.
func TestClaimedSourceSetsDistinctHintsAcrossWindows(t *testing.T) {
	t.Parallel()

	firstAt := time.Date(2026, time.May, 31, 18, 30, 0, 0, time.UTC)
	issue := Issue{
		ID:        "10001",
		Key:       "OPS-123",
		Summary:   "Investigate checkout alert",
		Status:    Reference{ID: "3", Name: "In Progress"},
		Project:   Reference{ID: "10000", Key: "OPS", Name: "Operations"},
		UpdatedAt: firstAt,
		BrowseURL: "https://example.atlassian.net/browse/OPS-123",
	}
	now := firstAt
	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "jira-primary",
		Targets: []TargetConfig{{
			Provider: ProviderJiraCloud,
			ScopeID:  "jira:site:example",
			SiteID:   "example.atlassian.net",
			BaseURL:  "https://example.atlassian.net",
			Token:    "token",
		}},
		ClientFactory: func(TargetConfig) (EvidenceClient, error) {
			return fakeEvidenceClient{result: CollectionResult{
				Issues:     []Issue{issue},
				ObservedAt: now,
			}}, nil
		},
		Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewClaimedSource() error = %v, want nil", err)
	}

	first, ok, err := source.NextClaimed(context.Background(), freshnessSourceItem("jira:generation-1"))
	if err != nil {
		t.Fatalf("NextClaimed() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("NextClaimed() ok = false, want true")
	}
	now = firstAt.Add(2 * time.Hour)
	second, ok, err := source.NextClaimed(context.Background(), freshnessSourceItem("jira:generation-2"))
	if err != nil {
		t.Fatalf("NextClaimed() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("NextClaimed() ok = false, want true")
	}
	if first.Generation.FreshnessHint == "" || second.Generation.FreshnessHint == "" {
		t.Fatalf("FreshnessHint blank: %q vs %q, want multivariate digests",
			first.Generation.FreshnessHint, second.Generation.FreshnessHint)
	}
	if first.Generation.FreshnessHint == second.Generation.FreshnessHint {
		t.Fatalf("FreshnessHint did not change across windows: %q", first.Generation.FreshnessHint)
	}
}

// TestClaimedSourceKeepsHintForByteIdenticalRetry proves idempotent retry: the
// same content under the same window shares one hint.
func TestClaimedSourceKeepsHintForByteIdenticalRetry(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.May, 31, 18, 30, 0, 0, time.UTC)
	issue := Issue{
		ID:        "10001",
		Key:       "OPS-123",
		Summary:   "Investigate checkout alert",
		Status:    Reference{ID: "3", Name: "In Progress"},
		Project:   Reference{ID: "10000", Key: "OPS", Name: "Operations"},
		UpdatedAt: observedAt,
		BrowseURL: "https://example.atlassian.net/browse/OPS-123",
	}
	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "jira-primary",
		Targets: []TargetConfig{{
			Provider: ProviderJiraCloud,
			ScopeID:  "jira:site:example",
			SiteID:   "example.atlassian.net",
			BaseURL:  "https://example.atlassian.net",
			Token:    "token",
		}},
		ClientFactory: func(TargetConfig) (EvidenceClient, error) {
			return fakeEvidenceClient{result: CollectionResult{
				Issues:     []Issue{issue},
				ObservedAt: observedAt,
			}}, nil
		},
		Now: func() time.Time { return observedAt },
	})
	if err != nil {
		t.Fatalf("NewClaimedSource() error = %v, want nil", err)
	}

	first, _, err := source.NextClaimed(context.Background(), freshnessSourceItem("jira:generation-1"))
	if err != nil {
		t.Fatalf("NextClaimed() error = %v, want nil", err)
	}
	second, _, err := source.NextClaimed(context.Background(), freshnessSourceItem("jira:generation-1"))
	if err != nil {
		t.Fatalf("NextClaimed() error = %v, want nil", err)
	}
	if first.Generation.FreshnessHint != second.Generation.FreshnessHint {
		t.Fatalf("FreshnessHint changed for a byte-identical retry: %q vs %q",
			first.Generation.FreshnessHint, second.Generation.FreshnessHint)
	}
}

// TestClaimedSourceMapsAllowEmptySuccessorToTrustedDeletion proves the escape
// hatch wiring: only a target that explicitly attests its deletions carries
// the trusted-deletion signal the store backstop honors.
func TestClaimedSourceMapsAllowEmptySuccessorToTrustedDeletion(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.May, 31, 18, 30, 0, 0, time.UTC)
	collect := func(t *testing.T, allowEmpty bool) scope.ScopeGeneration {
		t.Helper()
		source, err := NewClaimedSource(SourceConfig{
			CollectorInstanceID: "jira-primary",
			Targets: []TargetConfig{{
				Provider:            ProviderJiraCloud,
				ScopeID:             "jira:site:example",
				SiteID:              "example.atlassian.net",
				BaseURL:             "https://example.atlassian.net",
				Token:               "token",
				AllowEmptySuccessor: allowEmpty,
			}},
			ClientFactory: func(TargetConfig) (EvidenceClient, error) {
				return fakeEvidenceClient{result: CollectionResult{ObservedAt: observedAt}}, nil
			},
			Now: func() time.Time { return observedAt },
		})
		if err != nil {
			t.Fatalf("NewClaimedSource() error = %v, want nil", err)
		}
		collected, ok, err := source.NextClaimed(context.Background(), freshnessSourceItem("jira:generation-empty"))
		if err != nil {
			t.Fatalf("NextClaimed() error = %v, want nil", err)
		}
		if !ok {
			t.Fatal("NextClaimed() ok = false, want true")
		}
		return collected.Generation
	}
	if got := collect(t, false); got.TrustedDeletion {
		t.Fatal("TrustedDeletion = true without AllowEmptySuccessor, want false")
	}
	if got := collect(t, true); !got.TrustedDeletion {
		t.Fatal("TrustedDeletion = false with AllowEmptySuccessor, want true")
	}
}
