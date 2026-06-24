// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package jira

import (
	"context"
	"errors"
	"testing"
	"time"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestClaimedSourceEmitsWorkItemEvidenceFacts(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.May, 31, 18, 30, 0, 0, time.UTC)
	client := fakeEvidenceClient{result: CollectionResult{
		Issues: []Issue{{
			ID:        "10001",
			Key:       "OPS-123",
			Summary:   "Investigate checkout alert",
			Status:    Reference{ID: "3", Name: "In Progress"},
			Project:   Reference{ID: "10000", Key: "OPS", Name: "Operations"},
			UpdatedAt: observedAt,
			BrowseURL: "https://example.atlassian.net/browse/OPS-123",
		}},
		Transitions: map[string][]Transition{"10001": {{
			ID:        "20001",
			IssueID:   "10001",
			IssueKey:  "OPS-123",
			Field:     "status",
			From:      "To Do",
			To:        "In Progress",
			CreatedAt: observedAt.Add(-time.Hour),
		}}},
		ExternalLinks: map[string][]ExternalLink{"10001": {{
			ID:       "30001",
			IssueID:  "10001",
			IssueKey: "OPS-123",
			Object:   LinkObject{URL: "https://github.com/example/app/pull/42", Title: "PR 42"},
		}}},
		ObservedAt: observedAt,
	}}
	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "jira-primary",
		Targets: []TargetConfig{{
			Provider: ProviderJiraCloud,
			ScopeID:  "jira:site:example",
			SiteID:   "example.atlassian.net",
			BaseURL:  "https://example.atlassian.net",
			Token:    "token",
		}},
		ClientFactory: func(TargetConfig) (EvidenceClient, error) { return client, nil },
		Now:           func() time.Time { return observedAt },
	})
	if err != nil {
		t.Fatalf("NewClaimedSource() error = %v, want nil", err)
	}

	collected, ok, err := source.NextClaimed(context.Background(), workflow.WorkItem{
		CollectorKind:       scope.CollectorJira,
		CollectorInstanceID: "jira-primary",
		ScopeID:             "jira:site:example",
		GenerationID:        "jira:generation-1",
		CurrentFencingToken: 11,
		CurrentClaimID:      "claim-1",
		WorkItemID:          "work-1",
		RunID:               "run-1",
		SourceSystem:        string(scope.CollectorJira),
		AcceptanceUnitID:    "jira:site:example",
		SourceRunID:         "jira:generation-1",
		Status:              workflow.WorkItemStatusClaimed,
		CurrentOwnerID:      "owner-1",
		LastClaimedAt:       observedAt,
	})
	if err != nil {
		t.Fatalf("NextClaimed() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("NextClaimed() ok = false, want true")
	}
	if collected.Scope.ScopeKind != scope.KindJiraSite {
		t.Fatalf("ScopeKind = %q, want %q", collected.Scope.ScopeKind, scope.KindJiraSite)
	}
	var kinds []string
	for env := range collected.Facts {
		kinds = append(kinds, env.FactKind)
	}
	wantKinds := []string{
		facts.WorkItemRecordFactKind,
		facts.WorkItemTransitionFactKind,
		facts.WorkItemExternalLinkFactKind,
	}
	for i, want := range wantKinds {
		if kinds[i] != want {
			t.Fatalf("fact kind[%d] = %q, want %q; all %#v", i, kinds[i], want, kinds)
		}
	}
}

func TestClaimedSourceTreatsEmptyProjectAsSuccessfulEmptyGeneration(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.May, 31, 18, 30, 0, 0, time.UTC)
	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "jira-primary",
		Targets: []TargetConfig{{
			Provider: ProviderJiraCloud,
			ScopeID:  "jira:site:example",
			SiteID:   "example.atlassian.net",
			Token:    "token",
		}},
		ClientFactory: func(TargetConfig) (EvidenceClient, error) {
			return fakeEvidenceClient{result: CollectionResult{ObservedAt: observedAt}}, nil
		},
		Now: func() time.Time { return observedAt },
	})
	if err != nil {
		t.Fatalf("NewClaimedSource() error = %v, want nil", err)
	}

	collected, ok, err := source.NextClaimed(context.Background(), workflow.WorkItem{
		CollectorKind:       scope.CollectorJira,
		CollectorInstanceID: "jira-primary",
		ScopeID:             "jira:site:example",
		GenerationID:        "jira:generation-empty",
	})
	if err != nil {
		t.Fatalf("NextClaimed() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("NextClaimed() ok = false, want true")
	}
	if collected.FactCount != 0 {
		t.Fatalf("FactCount = %d, want 0", collected.FactCount)
	}
}

func TestProviderFailuresClassifyPermissionHiddenDeletedAndArchived(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want FailureClass
	}{
		{name: "permission hidden", err: JiraError{StatusCode: 403}, want: FailurePermissionHidden},
		{name: "deleted", err: JiraError{StatusCode: 404}, want: FailureDeleted},
		{name: "archived", err: ErrArchivedIssue, want: FailureArchived},
		{name: "rate limited", err: JiraError{StatusCode: 429}, want: FailureRateLimited},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := classifiedProviderFailure(tt.err)
			if got.FailureClass() != tt.want {
				t.Fatalf("FailureClass = %q, want %q", got.FailureClass(), tt.want)
			}
			if !errors.Is(got, tt.err) && tt.err != ErrArchivedIssue {
				t.Fatalf("classified failure does not wrap original error")
			}
		})
	}
}

func TestClaimedSourceRecordsFetchStatsOnSpan(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.May, 31, 18, 30, 0, 0, time.UTC)
	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder))
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
				Issues: []Issue{{
					ID:        "10001",
					Key:       "OPS-123",
					Status:    Reference{ID: "3", Name: "In Progress"},
					Project:   Reference{ID: "10000", Key: "OPS"},
					UpdatedAt: observedAt,
				}},
				Stats: CollectionStats{
					SearchPages:              2,
					ChangelogPages:           3,
					RemoteLinkPages:          1,
					IssuesEmitted:            1,
					ChangelogEventsEmitted:   2,
					RemoteLinksEmitted:       1,
					RemoteLinksRejected:      1,
					UnsupportedProviderLinks: 1,
				},
				ObservedAt: observedAt,
			}}, nil
		},
		Now:    func() time.Time { return observedAt },
		Tracer: tracerProvider.Tracer("test"),
	})
	if err != nil {
		t.Fatalf("NewClaimedSource() error = %v, want nil", err)
	}

	_, ok, err := source.NextClaimed(context.Background(), workflow.WorkItem{
		CollectorKind:       scope.CollectorJira,
		CollectorInstanceID: "jira-primary",
		ScopeID:             "jira:site:example",
		GenerationID:        "jira:generation-1",
	})
	if err != nil {
		t.Fatalf("NextClaimed() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("NextClaimed() ok = false, want true")
	}
	spans := spanRecorder.Ended()
	if got := spanIntAttribute(t, spans, telemetry.SpanJiraFetch, telemetry.SpanAttrJiraSearchPages); got != 2 {
		t.Fatalf("jira.search_pages = %d, want 2", got)
	}
	if got := spanIntAttribute(t, spans, telemetry.SpanJiraFetch, telemetry.SpanAttrJiraRemoteLinksRejected); got != 1 {
		t.Fatalf("jira.remote_links_rejected = %d, want 1", got)
	}
	if got := spanIntAttribute(t, spans, telemetry.SpanJiraFetch, telemetry.SpanAttrJiraUnsupportedProviderLinks); got != 1 {
		t.Fatalf("jira.unsupported_provider_links = %d, want 1", got)
	}
}

type fakeEvidenceClient struct {
	result CollectionResult
	err    error
}

func (f fakeEvidenceClient) CollectWorkItemEvidence(context.Context, TargetConfig, CollectionWindow) (CollectionResult, error) {
	return f.result, f.err
}

func spanIntAttribute(t *testing.T, spans []sdktrace.ReadOnlySpan, spanName string, key string) int64 {
	t.Helper()
	for _, span := range spans {
		if span.Name() != spanName {
			continue
		}
		for _, attr := range span.Attributes() {
			if string(attr.Key) == key {
				return attr.Value.AsInt64()
			}
		}
		t.Fatalf("span %q missing attribute %q", spanName, key)
	}
	t.Fatalf("missing span %q", spanName)
	return 0
}
