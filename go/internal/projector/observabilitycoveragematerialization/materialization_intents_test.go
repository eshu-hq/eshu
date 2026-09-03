// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package observabilitycoveragematerialization

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
	return BuildObservabilityCoverageMaterializationReducerIntent(
		testScopeID, testGenerationID, projectorintent.NewFactLookup(envelopes),
	)
}

// TestBuildObservabilityCoverageMaterializationReducerIntentAnchorsToObservabilityResource
// proves the whole intent value: a generic aws_resource fact earlier in input
// order is not a trigger, so the intent anchors to the first aws_resource
// whose resource_type is in the observabilityResourceTypes set, and carries
// the shared aws_resource_materialization entity key the reducer's
// canonical-nodes readiness gate resolves against.
func TestBuildObservabilityCoverageMaterializationReducerIntentAnchorsToObservabilityResource(t *testing.T) {
	t.Parallel()

	intent, ok := buildIntent(t, []facts.Envelope{
		awsResourceFactEnvelope("fact-lambda", "aws_lambda_function"),
		awsResourceFactEnvelope("fact-alarm", "aws_cloudwatch_alarm"),
		awsResourceFactEnvelope("fact-alarm-2", "aws_cloudwatch_dashboard"),
	})
	if !ok {
		t.Fatal("BuildObservabilityCoverageMaterializationReducerIntent() ok = false, want intent")
	}
	if got, want := intent.ScopeID, testScopeID; got != want {
		t.Fatalf("intent.ScopeID = %q, want %q", got, want)
	}
	if got, want := intent.GenerationID, testGenerationID; got != want {
		t.Fatalf("intent.GenerationID = %q, want %q", got, want)
	}
	if got, want := intent.Domain, reducer.DomainObservabilityCoverageMaterialization; got != want {
		t.Fatalf("intent.Domain = %q, want %q", got, want)
	}
	if got, want := intent.EntityKey, "aws_resource_materialization:"+testScopeID; got != want {
		t.Fatalf("intent.EntityKey = %q, want %q", got, want)
	}
	if got, want := intent.FactID, "fact-alarm"; got != want {
		t.Fatalf("intent.FactID = %q, want the first observability resource fact", got)
	}
	if got, want := intent.Reason, "aws observability resource facts observed"; got != want {
		t.Fatalf("intent.Reason = %q, want %q", got, want)
	}
	if got, want := intent.SourceSystem, "aws"; got != want {
		t.Fatalf("intent.SourceSystem = %q, want %q", got, want)
	}
}

// TestBuildObservabilityCoverageMaterializationReducerIntentAcceptsEveryObservabilityType
// pins the closed set: every resource type in observabilityResourceTypes must
// trigger on its own, so a type silently dropped from the set fails here
// rather than silently stopping COVERS edge projection for that signal.
func TestBuildObservabilityCoverageMaterializationReducerIntentAcceptsEveryObservabilityType(t *testing.T) {
	t.Parallel()

	for _, resourceType := range []string{
		"aws_cloudwatch_alarm",
		"aws_cloudwatch_composite_alarm",
		"aws_cloudwatch_dashboard",
		"aws_cloudwatch_logs_log_group",
		"aws_xray_sampling_rule",
		"aws_xray_group",
	} {
		if _, ok := buildIntent(t, []facts.Envelope{awsResourceFactEnvelope("fact-1", resourceType)}); !ok {
			t.Fatalf("resource type %q did not trigger the materialization intent", resourceType)
		}
	}
}

// TestBuildObservabilityCoverageMaterializationReducerIntentWithoutObservabilityResource
// proves a generation carrying only non-observability aws_resource facts
// enqueues nothing: without an observability object there can be no COVERS
// edge to materialize.
func TestBuildObservabilityCoverageMaterializationReducerIntentWithoutObservabilityResource(t *testing.T) {
	t.Parallel()

	if _, ok := buildIntent(t, []facts.Envelope{
		awsResourceFactEnvelope("fact-lambda", "aws_lambda_function"),
	}); ok {
		t.Fatal("BuildObservabilityCoverageMaterializationReducerIntent() ok = true, want no intent")
	}
}

// TestBuildObservabilityCoverageMaterializationReducerIntentSkipsUndecodableResource
// proves an aws_resource fact this package cannot decode is treated as
// no-match rather than as an error: awsResourceTypeForEnvelope swallows the
// decode failure to an empty resource type, which never matches the closed
// set. Root's quarantine path owns flagging the invalid fact.
func TestBuildObservabilityCoverageMaterializationReducerIntentSkipsUndecodableResource(t *testing.T) {
	t.Parallel()

	broken := awsResourceFactEnvelope("fact-broken-alarm", "aws_cloudwatch_alarm")
	delete(broken.Payload, "resource_id")
	if _, ok := buildIntent(t, []facts.Envelope{broken}); ok {
		t.Fatal("BuildObservabilityCoverageMaterializationReducerIntent() ok = true, want no intent for an undecodable payload")
	}
}

// TestBuildObservabilityCoverageMaterializationReducerIntentIgnoresNonAWSKinds
// proves the trigger is aws_resource-only. Observability source facts (the
// sibling correlation family's other branch) are not a materialization
// trigger: a COVERS edge needs an AWS-native observability object.
func TestBuildObservabilityCoverageMaterializationReducerIntentIgnoresNonAWSKinds(t *testing.T) {
	t.Parallel()

	version, _ := facts.ObservabilitySchemaVersion(facts.ObservabilityDeclaredDashboardFactKind)
	dashboard := facts.Envelope{
		FactID:        "observability-dashboard-1",
		ScopeID:       testScopeID,
		GenerationID:  testGenerationID,
		FactKind:      facts.ObservabilityDeclaredDashboardFactKind,
		SchemaVersion: version,
		CollectorKind: "git",
		SourceRef:     facts.Ref{SourceSystem: "git"},
	}
	if _, ok := buildIntent(t, []facts.Envelope{dashboard}); ok {
		t.Fatal("BuildObservabilityCoverageMaterializationReducerIntent() ok = true, want no intent for an observability source fact")
	}
}

// TestObservabilityCoverageMaterializationSourceSystemTiers pins the label
// this family emits through the shared two-tier projectorintent.SourceSystem
// helper: the trimmed SourceRef.SourceSystem first, then the trimmed
// CollectorKind. Unlike the sibling correlation family, this family has no
// third literal fallback -- a trigger fact with neither label yields an empty
// SourceSystem, which is the pre-extraction behavior.
func TestObservabilityCoverageMaterializationSourceSystemTiers(t *testing.T) {
	t.Parallel()

	collectorOnly := awsResourceFactEnvelope("fact-alarm", "aws_cloudwatch_alarm")
	collectorOnly.SourceRef.SourceSystem = ""
	intent, ok := buildIntent(t, []facts.Envelope{collectorOnly})
	if !ok {
		t.Fatal("BuildObservabilityCoverageMaterializationReducerIntent() ok = false, want intent")
	}
	if got, want := intent.SourceSystem, "aws_cloud"; got != want {
		t.Fatalf("intent.SourceSystem = %q, want the CollectorKind fallback %q", got, want)
	}

	unlabeled := awsResourceFactEnvelope("fact-alarm", "aws_cloudwatch_alarm")
	unlabeled.SourceRef.SourceSystem = ""
	unlabeled.CollectorKind = ""
	intent, ok = buildIntent(t, []facts.Envelope{unlabeled})
	if !ok {
		t.Fatal("BuildObservabilityCoverageMaterializationReducerIntent() ok = false, want intent")
	}
	if intent.SourceSystem != "" {
		t.Fatalf("intent.SourceSystem = %q, want empty: this family has no literal third tier", intent.SourceSystem)
	}
}
