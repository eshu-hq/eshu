// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package tempo

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestClaimedSourceEmitsObservedTempoFacts(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "collector-1",
		Now:                 func() time.Time { return observedAt },
		ClientFactory: func(TargetConfig) (EvidenceClient, error) {
			return stubEvidenceClient{result: CollectionResult{
				Source: SourceInstance{
					Provider:         ProviderTempo,
					SourceInstanceID: "tempo-main",
				},
				Signals: []TraceSignal{{
					ProviderObjectID: "tagset-1",
					SignalKind:       SignalKindTagSet,
					TagKeys:          []string{"service.name"},
				}},
				Warnings: []Warning{{
					ResourceClass: ResourceClassTraceSignal,
					ResourceID:    "tag:service.name",
					Reason:        WarningManualProviderResource,
				}},
				ObservedAt: observedAt,
			}}, nil
		},
		Targets: []TargetConfig{{
			ScopeID:    "tempo-prod",
			InstanceID: "tempo-main",
			BaseURL:    "https://tempo.example.test",
			Enabled:    true,
		}},
	})
	if err != nil {
		t.Fatalf("NewClaimedSource() error = %v", err)
	}

	generation, ok, err := source.NextClaimed(t.Context(), workflow.WorkItem{
		CollectorInstanceID: "collector-1",
		CollectorKind:       CollectorKind,
		ScopeID:             "tempo-prod",
		GenerationID:        "gen-1",
		CurrentFencingToken: 7,
	})
	if err != nil {
		t.Fatalf("NextClaimed() error = %v", err)
	}
	if !ok {
		t.Fatalf("NextClaimed() ok = false")
	}
	if generation.Scope.ScopeID != "tempo-prod" || generation.Scope.SourceSystem != CollectorKind {
		t.Fatalf("scope = %#v", generation.Scope)
	}
	if generation.Generation.FreshnessHint != "tempo_observed_metadata" {
		t.Fatalf("FreshnessHint = %q", generation.Generation.FreshnessHint)
	}

	var kinds []string
	for env := range generation.Facts {
		kinds = append(kinds, env.FactKind)
		if env.CollectorKind != CollectorKind {
			t.Fatalf("CollectorKind = %q, want %q", env.CollectorKind, CollectorKind)
		}
		if env.SourceConfidence != facts.SourceConfidenceReported {
			t.Fatalf("SourceConfidence = %q, want reported", env.SourceConfidence)
		}
	}
	want := []string{
		facts.ObservabilitySourceInstanceFactKind,
		facts.ObservabilityObservedTraceSignalFactKind,
		facts.ObservabilityCoverageWarningFactKind,
	}
	if !slices.Equal(kinds, want) {
		t.Fatalf("fact kinds = %#v, want %#v", kinds, want)
	}
}

func TestClaimedSourceClassifiesRateLimitFailures(t *testing.T) {
	t.Parallel()

	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "collector-1",
		ClientFactory: func(TargetConfig) (EvidenceClient, error) {
			return stubEvidenceClient{err: ProviderHTTPError{StatusCode: 429, Message: "rate limited tenant-secret-prod"}}, nil
		},
		Targets: []TargetConfig{{
			ScopeID:    "tempo-prod",
			InstanceID: "tempo-main",
			BaseURL:    "https://tempo.example.test",
			Enabled:    true,
		}},
	})
	if err != nil {
		t.Fatalf("NewClaimedSource() error = %v", err)
	}

	_, ok, err := source.NextClaimed(t.Context(), workflow.WorkItem{
		CollectorInstanceID: "collector-1",
		CollectorKind:       CollectorKind,
		ScopeID:             "tempo-prod",
		GenerationID:        "gen-1",
	})
	if ok {
		t.Fatalf("NextClaimed() ok = true, want failure")
	}
	var failure ProviderFailure
	if !errors.As(err, &failure) {
		t.Fatalf("NextClaimed() error = %T, want ProviderFailure", err)
	}
	if got := failure.FailureClass(); got != FailureRateLimited {
		t.Fatalf("FailureClass = %q, want %q", got, FailureRateLimited)
	}
	if err.Error() == "rate limited tenant-secret-prod" {
		t.Fatalf("provider failure leaked raw response body")
	}
}

type stubEvidenceClient struct {
	result CollectionResult
	err    error
}

func (c stubEvidenceClient) CollectObservedMetadata(context.Context, TargetConfig) (CollectionResult, error) {
	return c.result, c.err
}
