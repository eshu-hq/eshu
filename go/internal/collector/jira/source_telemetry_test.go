// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package jira

import (
	"context"
	"testing"
	"time"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestClaimedSourceRecordsRateLimitAndStaleWindowStatsOnSpan(t *testing.T) {
	t.Parallel()

	staleNow := time.Now().UTC().Add(-2 * time.Hour)
	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder))
	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "jira-primary",
		Targets: []TargetConfig{{
			Provider:        ProviderJiraCloud,
			ScopeID:         "jira:site:example",
			SiteID:          "example.atlassian.net",
			BaseURL:         "https://example.atlassian.net",
			Token:           "token",
			UpdatedLookback: time.Hour,
		}},
		ClientFactory: func(TargetConfig) (EvidenceClient, error) {
			return fakeEvidenceClient{err: JiraError{
				StatusCode:      429,
				RetryAfter:      7 * time.Second,
				RateLimitReason: "jira-burst-based",
			}}, nil
		},
		Now:    func() time.Time { return staleNow },
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
	if err == nil {
		t.Fatal("NextClaimed() error = nil, want rate limit failure")
	}
	if ok {
		t.Fatal("NextClaimed() ok = true, want false")
	}
	spans := spanRecorder.Ended()
	if got := spanIntAttribute(t, spans, telemetry.SpanJiraFetch, telemetry.SpanAttrJiraRateLimits); got != 1 {
		t.Fatalf("jira.rate_limits = %d, want 1", got)
	}
	if got := spanIntAttribute(t, spans, telemetry.SpanJiraFetch, telemetry.SpanAttrJiraRetryAfterSeconds); got != 7 {
		t.Fatalf("jira.retry_after_seconds = %d, want 7", got)
	}
	if got := spanIntAttribute(t, spans, telemetry.SpanJiraFetch, telemetry.SpanAttrJiraStaleWindows); got != 1 {
		t.Fatalf("jira.stale_windows = %d, want 1", got)
	}
}
