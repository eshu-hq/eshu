// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package loki

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestObservedLogSignalEnvelopeRedactsLabelValuesAndQueries(t *testing.T) {
	t.Parallel()

	ctx := testEnvelopeContext()
	signal := LogSignal{
		ProviderObjectID:   "stream-1",
		SignalKind:         SignalKindSeries,
		LabelKeys:          []string{"app", "namespace", "trace_id"},
		LabelValueCounts:   map[string]int{"app": 2, "trace_id": 2500},
		LabelValueHashes:   map[string][]string{"app": {"sha256:bounded"}},
		SeriesFingerprint:  "sha256:series",
		LastSeenAt:         time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC),
		DeclaredMatchState: MatchStateMatchedDeclared,
		Outcome:            OutcomeObserved,
		FreshnessState:     FreshnessCurrent,
		ManuallyCreated:    true,
	}

	env, err := NewObservedLogSignalEnvelope(ctx, signal)
	if err != nil {
		t.Fatalf("NewObservedLogSignalEnvelope() error = %v, want nil", err)
	}

	assertObservabilityEnvelope(t, env, facts.ObservabilityObservedLogSignalFactKind)
	assertPayload(t, env, "source_class", SourceClassObserved)
	assertPayload(t, env, "source_kind", SourceKindLoki)
	assertPayload(t, env, "signal_kind", SignalKindSeries)
	assertPayload(t, env, "declared_match_state", MatchStateMatchedDeclared)
	assertPayload(t, env, "freshness_state", FreshnessCurrent)
	assertPayload(t, env, "outcome", OutcomeObserved)
	assertPayload(t, env, "manually_created", true)
	assertPayloadKeysAbsent(t, env, "label_values", "logql", "query", "line", "log_line")
	assertPayloadForbidden(t, env, "checkout-prod", "trace-123", "{app=\"checkout\"}", "payment failed")
}

func TestObservedRuleEnvelopeDropsLogQLAndAnnotationValues(t *testing.T) {
	t.Parallel()

	ctx := testEnvelopeContext()
	rule := Rule{
		ProviderObjectID:   "prod/checkout.rules:HighLogErrors",
		Namespace:          "prod",
		GroupName:          "checkout.rules",
		RuleName:           "HighLogErrors",
		RuleType:           RuleTypeAlerting,
		Query:              "sum(rate({app=\"checkout\"} |= \"payment failed\" [5m])) > 0",
		QueryRedacted:      true,
		LabelKeys:          []string{"severity", "team"},
		AnnotationKeys:     []string{"summary"},
		LastEvaluationAt:   time.Date(2026, 6, 1, 12, 0, 5, 0, time.UTC),
		DeclaredMatchState: MatchStateMatchedDeclared,
	}

	env, err := NewObservedRuleEnvelope(ctx, rule)
	if err != nil {
		t.Fatalf("NewObservedRuleEnvelope() error = %v, want nil", err)
	}

	assertObservabilityEnvelope(t, env, facts.ObservabilityObservedRuleFactKind)
	assertPayload(t, env, "namespace", "prod")
	assertPayload(t, env, "rule_group", "checkout.rules")
	assertPayload(t, env, "rule_name", "HighLogErrors")
	assertPayload(t, env, "query_redacted", true)
	assertPayloadKeysAbsent(t, env, "query", "labels", "annotations")
	assertPayloadForbidden(t, env, "payment failed", "{app=\"checkout\"}", "Checkout errors")
}

func TestCoverageWarningEnvelopePreservesHighCardinalityState(t *testing.T) {
	t.Parallel()

	ctx := testEnvelopeContext()
	warning := Warning{
		ResourceClass: ResourceClassLogSignal,
		ResourceID:    "label:trace_id",
		Reason:        WarningHighCardinality,
	}

	env, err := NewCoverageWarningEnvelope(ctx, warning)
	if err != nil {
		t.Fatalf("NewCoverageWarningEnvelope() error = %v, want nil", err)
	}

	assertObservabilityEnvelope(t, env, facts.ObservabilityCoverageWarningFactKind)
	assertPayload(t, env, "warning_kind", WarningHighCardinality)
	assertPayload(t, env, "outcome", OutcomeRejected)
	assertPayload(t, env, "freshness_state", FreshnessUnknown)
}

func testEnvelopeContext() EnvelopeContext {
	return EnvelopeContext{
		ScopeID:             "loki:tenant:prod",
		GenerationID:        "generation-2026-06-01T12:00:00Z",
		CollectorInstanceID: "collector-loki-prod",
		FencingToken:        42,
		ObservedAt:          time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC),
		SourceInstanceID:    "loki-prod",
	}
}

func assertObservabilityEnvelope(t *testing.T, env facts.Envelope, kind string) {
	t.Helper()
	if env.FactKind != kind {
		t.Fatalf("FactKind = %q, want %q", env.FactKind, kind)
	}
	if env.SchemaVersion != facts.ObservabilitySchemaVersionV1 {
		t.Fatalf("SchemaVersion = %q, want %q", env.SchemaVersion, facts.ObservabilitySchemaVersionV1)
	}
	if env.CollectorKind != CollectorKind {
		t.Fatalf("CollectorKind = %q, want %q", env.CollectorKind, CollectorKind)
	}
	if env.SourceConfidence != facts.SourceConfidenceReported {
		t.Fatalf("SourceConfidence = %q, want %q", env.SourceConfidence, facts.SourceConfidenceReported)
	}
	if env.SourceRef.SourceURI != "" {
		t.Fatalf("SourceRef.SourceURI = %q, want redacted empty URI", env.SourceRef.SourceURI)
	}
}

func assertPayload(t *testing.T, env facts.Envelope, key string, want any) {
	t.Helper()
	if got := env.Payload[key]; got != want {
		t.Fatalf("%s payload[%s] = %#v, want %#v in %#v", env.FactKind, key, got, want, env.Payload)
	}
}

func assertPayloadForbidden(t *testing.T, env facts.Envelope, forbidden ...string) {
	t.Helper()
	rendered := strings.ToLower(fmt.Sprint(env.Payload))
	for _, value := range forbidden {
		if strings.Contains(rendered, strings.ToLower(value)) {
			t.Fatalf("%s payload leaks forbidden value %q: %#v", env.FactKind, value, env.Payload)
		}
	}
}

func assertPayloadKeysAbsent(t *testing.T, env facts.Envelope, forbidden ...string) {
	t.Helper()
	for _, key := range forbidden {
		if _, exists := env.Payload[key]; exists {
			t.Fatalf("%s payload has forbidden key %q: %#v", env.FactKind, key, env.Payload)
		}
	}
}
