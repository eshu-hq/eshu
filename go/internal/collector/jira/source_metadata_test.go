// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package jira

import (
	"context"
	"testing"
	"time"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestClaimedSourceEmitsWorkItemMetadataFacts(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.May, 31, 18, 30, 0, 0, time.UTC)
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
				Projects: []ProjectMetadata{{ID: "10000", Key: "OPS", Name: "Private Operations"}},
				IssueTypes: []IssueTypeMetadata{{
					ID: "10002", Name: "Incident", ProjectID: "10000",
				}},
				Statuses: []StatusMetadata{{
					ID: "3", Name: "In Progress", StatusCategory: "IN_PROGRESS", ProjectID: "10000",
				}},
				Workflows: []WorkflowMetadata{{
					ID: "workflow-1", Name: "Incident Workflow",
					Transitions: []WorkflowTransitionMetadata{{ID: "41", Name: "Resolve", ToStatusReference: "done-ref"}},
				}},
				Fields: []FieldMetadata{{
					ID: "customfield_10042", Name: "Customer account owner",
					Schema: FieldSchema{Type: "array", Items: "user", CustomID: "10042"},
				}},
				MetadataWarnings: []MetadataWarning{{
					MetadataType: "workflow", Reason: "permission_hidden", FailureClass: string(FailurePermissionHidden),
				}},
				ObservedAt: observedAt,
			}}, nil
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
		GenerationID:        "jira:generation-1",
	})
	if err != nil {
		t.Fatalf("NextClaimed() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("NextClaimed() ok = false, want true")
	}
	kinds := map[string]int{}
	for env := range collected.Facts {
		kinds[env.FactKind]++
	}
	for _, want := range []string{
		facts.WorkItemProjectMetadataFactKind,
		facts.WorkItemIssueTypeMetadataFactKind,
		facts.WorkItemStatusMetadataFactKind,
		facts.WorkItemWorkflowMetadataFactKind,
		facts.WorkItemFieldMetadataFactKind,
		facts.WorkItemMetadataWarningFactKind,
	} {
		if kinds[want] != 1 {
			t.Fatalf("fact kind %q count = %d, want 1; all %#v", want, kinds[want], kinds)
		}
	}
}

func TestClaimedSourceRecordsMetadataStatsOnSpan(t *testing.T) {
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
				Stats: CollectionStats{
					MetadataPages:            3,
					MetadataObjectsScanned:   4,
					MetadataObjectsEmitted:   4,
					UnsupportedMetadata:      1,
					PermissionHiddenMetadata: 1,
					StaleMetadata:            1,
					MetadataRedactions:       2,
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
	if got := spanIntAttribute(t, spans, telemetry.SpanJiraFetch, telemetry.SpanAttrJiraMetadataObjectsEmitted); got != 4 {
		t.Fatalf("jira.metadata_objects_emitted = %d, want 4", got)
	}
	if got := spanIntAttribute(t, spans, telemetry.SpanJiraFetch, telemetry.SpanAttrJiraPermissionHiddenMetadata); got != 1 {
		t.Fatalf("jira.permission_hidden_metadata = %d, want 1", got)
	}
	if got := spanIntAttribute(t, spans, telemetry.SpanJiraFetch, telemetry.SpanAttrJiraMetadataRedactions); got != 2 {
		t.Fatalf("jira.metadata_redactions = %d, want 2", got)
	}
}
