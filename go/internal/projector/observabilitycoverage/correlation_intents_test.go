// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package observabilitycoverage

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	projectorintent "github.com/eshu-hq/eshu/go/internal/projector/intent"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

const (
	testScopeID      = "aws:123456789012:us-east-1:lambda"
	testGenerationID = "aws-generation-1"
)

func observabilitySourceFactEnvelope(factID, kind string) facts.Envelope {
	version, _ := facts.ObservabilitySchemaVersion(kind)
	return facts.Envelope{
		FactID:        factID,
		ScopeID:       testScopeID,
		GenerationID:  testGenerationID,
		FactKind:      kind,
		SchemaVersion: version,
		CollectorKind: "git",
		ObservedAt:    time.Date(2026, time.June, 1, 12, 0, 0, 0, time.UTC),
		SourceRef: facts.Ref{
			SourceSystem: "git",
		},
		Payload: map[string]any{
			"scope_id":          testScopeID,
			"generation_id":     testGenerationID,
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

func awsResourceFactEnvelope(factID, resourceType string) facts.Envelope {
	return facts.Envelope{
		FactID:        factID,
		ScopeID:       testScopeID,
		GenerationID:  testGenerationID,
		FactKind:      facts.AWSResourceFactKind,
		SchemaVersion: facts.AWSResourceSchemaVersion,
		CollectorKind: "aws_cloud",
		ObservedAt:    time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC),
		SourceRef: facts.Ref{
			SourceSystem: "aws",
		},
		Payload: map[string]any{
			"account_id":    "123456789012",
			"arn":           "arn:aws:cloudwatch:us-east-1:123456789012:alarm:cpu-high",
			"region":        "us-east-1",
			"resource_id":   "cpu-high",
			"resource_type": resourceType,
		},
	}
}

func buildIntent(t *testing.T, envelopes []facts.Envelope) (projectorintent.ReducerIntent, bool) {
	t.Helper()
	return BuildObservabilityCoverageCorrelationReducerIntent(
		testScopeID, testGenerationID, projectorintent.NewFactLookup(envelopes),
	)
}

// TestBuildObservabilityCoverageCorrelationReducerIntentForSourceFacts proves
// an observability source fact triggers the correlation intent, anchored to
// the earliest such fact in input order, with the source-ref-first label.
func TestBuildObservabilityCoverageCorrelationReducerIntentForSourceFacts(t *testing.T) {
	t.Parallel()

	intent, ok := buildIntent(t, []facts.Envelope{
		observabilitySourceFactEnvelope("observability-dashboard-1", facts.ObservabilityDeclaredDashboardFactKind),
		observabilitySourceFactEnvelope("observability-dashboard-2", facts.ObservabilityDeclaredDashboardFactKind),
	})
	if !ok {
		t.Fatal("BuildObservabilityCoverageCorrelationReducerIntent() ok = false, want intent")
	}
	if got, want := intent.Domain, reducer.DomainObservabilityCoverageCorrelation; got != want {
		t.Fatalf("intent.Domain = %q, want %q", got, want)
	}
	if got, want := intent.EntityKey, "observability_coverage_correlation:"+testScopeID; got != want {
		t.Fatalf("intent.EntityKey = %q, want %q", got, want)
	}
	if got, want := intent.FactID, "observability-dashboard-1"; got != want {
		t.Fatalf("intent.FactID = %q, want first observability source fact", got)
	}
	if got, want := intent.Reason, "observability source facts observed"; got != want {
		t.Fatalf("intent.Reason = %q, want %q", got, want)
	}
	if got, want := intent.SourceSystem, "git"; got != want {
		t.Fatalf("intent.SourceSystem = %q, want %q", got, want)
	}
}

// TestBuildObservabilityCoverageCorrelationReducerIntentForAWSObservabilityFacts
// proves the AWS branch: a generic aws_resource fact earlier in input order is
// not a trigger, so the intent anchors to the first aws_resource whose
// resource_type is in the observabilityResourceTypes set and carries the
// AWS-branch reason.
func TestBuildObservabilityCoverageCorrelationReducerIntentForAWSObservabilityFacts(t *testing.T) {
	t.Parallel()

	intent, ok := buildIntent(t, []facts.Envelope{
		awsResourceFactEnvelope("fact-lambda", "aws_lambda_function"),
		awsResourceFactEnvelope("fact-dashboard", "aws_cloudwatch_dashboard"),
	})
	if !ok {
		t.Fatal("BuildObservabilityCoverageCorrelationReducerIntent() ok = false, want intent")
	}
	if got, want := intent.EntityKey, "observability_coverage_correlation:"+testScopeID; got != want {
		t.Fatalf("intent.EntityKey = %q, want %q", got, want)
	}
	if got, want := intent.FactID, "fact-dashboard"; got != want {
		t.Fatalf("intent.FactID = %q, want first AWS observability fact", got)
	}
	if got, want := intent.Reason, "aws observability resource facts observed"; got != want {
		t.Fatalf("intent.Reason = %q, want %q", got, want)
	}
	if got, want := intent.SourceSystem, "aws"; got != want {
		t.Fatalf("intent.SourceSystem = %q, want %q", got, want)
	}
}

// TestBuildObservabilityCoverageCorrelationReducerIntentWithoutObservabilityResource
// proves a generation with only non-observability aws_resource facts enqueues
// nothing.
func TestBuildObservabilityCoverageCorrelationReducerIntentWithoutObservabilityResource(t *testing.T) {
	t.Parallel()

	if intent, ok := buildIntent(t, []facts.Envelope{
		awsResourceFactEnvelope("fact-lambda", "aws_lambda_function"),
	}); ok {
		t.Fatalf("unexpected observability_coverage_correlation intent without an observability resource: %#v", intent)
	}
}

// TestBuildObservabilityCoverageCorrelationReducerIntentSkipsSourceInstanceFacts
// pins the explicit observability_source.instance exclusion: the instance
// fact kind is registry-versioned like the source kinds but is deliberately
// not a correlation trigger.
func TestBuildObservabilityCoverageCorrelationReducerIntentSkipsSourceInstanceFacts(t *testing.T) {
	t.Parallel()

	if intent, ok := buildIntent(t, []facts.Envelope{
		observabilitySourceFactEnvelope("observability-instance-1", facts.ObservabilitySourceInstanceFactKind),
	}); ok {
		t.Fatalf("unexpected observability_coverage_correlation intent from observability_source.instance: %#v", intent)
	}
}

// TestBuildObservabilityCoverageCorrelationReducerIntentSkipsUndecodableAWSResource
// pins the decode-error-swallow branch: an aws_resource fact whose payload
// fails the typed decode (missing resource_id) yields an empty resource type,
// which never matches the observabilityResourceTypes set, so the fact is
// simply not a trigger rather than an error.
func TestBuildObservabilityCoverageCorrelationReducerIntentSkipsUndecodableAWSResource(t *testing.T) {
	t.Parallel()

	invalid := awsResourceFactEnvelope("fact-invalid-alarm", "aws_cloudwatch_alarm")
	delete(invalid.Payload, "resource_id")

	if intent, ok := buildIntent(t, []facts.Envelope{invalid}); ok {
		t.Fatalf("unexpected observability_coverage_correlation intent from undecodable aws_resource: %#v", intent)
	}
}

// TestObservabilitySourceSystemThirdTierFallback pins the family's literal
// third source-system tier: a trigger fact with a blank source ref AND blank
// collector kind labels the intent "observability". The shared two-tier
// projectorintent.SourceSystem returns an empty string for that envelope, so
// substituting it would silently relabel the intent — this test fails under
// that substitution.
func TestObservabilitySourceSystemThirdTierFallback(t *testing.T) {
	t.Parallel()

	unlabeled := observabilitySourceFactEnvelope("observability-dashboard-1", facts.ObservabilityDeclaredDashboardFactKind)
	unlabeled.CollectorKind = ""
	unlabeled.SourceRef = facts.Ref{}

	intent, ok := buildIntent(t, []facts.Envelope{unlabeled})
	if !ok {
		t.Fatal("BuildObservabilityCoverageCorrelationReducerIntent() ok = false, want intent")
	}
	if got, want := intent.SourceSystem, "observability"; got != want {
		t.Fatalf("intent.SourceSystem = %q, want the literal third-tier fallback %q", got, want)
	}
	if got := projectorintent.SourceSystem(unlabeled); got != "" {
		t.Fatalf("projectorintent.SourceSystem() = %q, want \"\" — the shared helper grew a third tier, revisit whether this family still needs its local copy", got)
	}
}
