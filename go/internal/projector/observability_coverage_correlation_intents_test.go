// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func observabilityCoverageFactEnvelope(
	factID string,
	scopeID string,
	generationID string,
	kind string,
) facts.Envelope {
	version, _ := facts.ObservabilitySchemaVersion(kind)
	return facts.Envelope{
		FactID:        factID,
		ScopeID:       scopeID,
		GenerationID:  generationID,
		FactKind:      kind,
		SchemaVersion: version,
		CollectorKind: "git",
		ObservedAt:    time.Date(2026, time.June, 1, 12, 0, 0, 0, time.UTC),
		SourceRef: facts.Ref{
			SourceSystem: "git",
		},
		Payload: map[string]any{
			"scope_id":          scopeID,
			"generation_id":     generationID,
			"provider":          "grafana",
			"source_class":      "declared",
			"source_kind":       "kubernetes",
			"dashboard_uid":     "checkout-latency",
			"freshness_state":   "current",
			"redaction_version": facts.ObservabilitySchemaVersionV1,
			"outcome":           "exact",
		},
	}
}

func TestBuildProjectionQueuesObservabilityCoverageCorrelationForSourceFacts(t *testing.T) {
	t.Parallel()

	scopeValue, generation := observabilityCoverageScopeAndGeneration()
	envelopes := []facts.Envelope{
		observabilityCoverageFactEnvelope(
			"observability-dashboard-1",
			scopeValue.ScopeID,
			generation.GenerationID,
			facts.ObservabilityDeclaredDashboardFactKind,
		),
	}

	projection, err := buildProjection(scopeValue, generation, envelopes)
	if err != nil {
		t.Fatalf("buildProjection() error = %v, want nil", err)
	}
	intent := intentForDomain(t, projection.reducerIntents, reducer.DomainObservabilityCoverageCorrelation)
	if got, want := intent.EntityKey, "observability_coverage_correlation:"+scopeValue.ScopeID; got != want {
		t.Fatalf("intent.EntityKey = %q, want %q", got, want)
	}
	if got, want := intent.FactID, "observability-dashboard-1"; got != want {
		t.Fatalf("intent.FactID = %q, want first observability source fact", got)
	}
	if got, want := intent.SourceSystem, "git"; got != want {
		t.Fatalf("intent.SourceSystem = %q, want %q", got, want)
	}
}

func TestBuildProjectionQueuesObservabilityCoverageCorrelationForAWSObservabilityFacts(t *testing.T) {
	t.Parallel()

	scopeValue, generation := observabilityCoverageScopeAndGeneration()
	envelopes := []facts.Envelope{
		awsResourceEnvelope("fact-lambda", scopeValue.ScopeID, generation.GenerationID),
		observabilityAWSResourceEnvelope("fact-dashboard", scopeValue.ScopeID, generation.GenerationID, "aws_cloudwatch_dashboard"),
	}

	projection, err := buildProjection(scopeValue, generation, envelopes)
	if err != nil {
		t.Fatalf("buildProjection() error = %v, want nil", err)
	}
	intent := intentForDomain(t, projection.reducerIntents, reducer.DomainObservabilityCoverageCorrelation)
	if got, want := intent.EntityKey, "observability_coverage_correlation:"+scopeValue.ScopeID; got != want {
		t.Fatalf("intent.EntityKey = %q, want %q", got, want)
	}
	if got, want := intent.FactID, "fact-dashboard"; got != want {
		t.Fatalf("intent.FactID = %q, want first AWS observability fact", got)
	}
	if got, want := intent.SourceSystem, "aws"; got != want {
		t.Fatalf("intent.SourceSystem = %q, want %q", got, want)
	}
}

func TestBuildProjectionDoesNotQueueObservabilityCoverageCorrelationWithoutObservabilityResource(t *testing.T) {
	t.Parallel()

	scopeValue, generation := observabilityCoverageScopeAndGeneration()
	envelopes := []facts.Envelope{
		awsResourceEnvelope("fact-lambda", scopeValue.ScopeID, generation.GenerationID),
	}

	projection, err := buildProjection(scopeValue, generation, envelopes)
	if err != nil {
		t.Fatalf("buildProjection() error = %v, want nil", err)
	}
	for _, intent := range projection.reducerIntents {
		if intent.Domain == reducer.DomainObservabilityCoverageCorrelation {
			t.Fatalf("unexpected observability_coverage_correlation intent without an observability resource")
		}
	}
}

func TestBuildProjectionRejectsUnsupportedObservabilitySchemaVersion(t *testing.T) {
	t.Parallel()

	scopeValue, generation := observabilityCoverageScopeAndGeneration()
	fact := observabilityCoverageFactEnvelope(
		"observability-dashboard-1",
		scopeValue.ScopeID,
		generation.GenerationID,
		facts.ObservabilityDeclaredDashboardFactKind,
	)
	fact.SchemaVersion = "0.0.0"

	if _, err := buildProjection(scopeValue, generation, []facts.Envelope{fact}); err == nil {
		t.Fatal("buildProjection() error = nil, want unsupported observability schema version")
	}
}
