// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package prometheusmimir

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestObservedTargetEnvelopeRedactsTargetAddressAndLabels(t *testing.T) {
	t.Parallel()

	ctx := testEnvelopeContext()
	target := Target{
		ProviderObjectID:   "target-1",
		ScrapePool:         "kubernetes-pods",
		Health:             "up",
		ScrapeURL:          "https://user:pass@10.0.0.1:9100/metrics",
		LabelKeys:          []string{"instance", "job", "pod"},
		DiscoveredKeys:     []string{"__address__", "__meta_kubernetes_pod_name"},
		LastScrapeAt:       time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC),
		DeclaredMatchState: MatchStateMatchedDeclared,
	}

	env, err := NewObservedTargetEnvelope(ctx, target)
	if err != nil {
		t.Fatalf("NewObservedTargetEnvelope() error = %v, want nil", err)
	}

	assertObservabilityEnvelope(t, env, facts.ObservabilityObservedTargetFactKind)
	assertPayload(t, env, "source_class", SourceClassObserved)
	assertPayload(t, env, "source_kind", SourceKindPrometheus)
	assertPayload(t, env, "scrape_pool", "kubernetes-pods")
	assertPayload(t, env, "health", "up")
	assertPayload(t, env, "scrape_url_present", true)
	assertPayload(t, env, "scrape_url_redacted", true)
	assertPayload(t, env, "declared_match_state", MatchStateMatchedDeclared)
	assertPayload(t, env, "freshness_state", FreshnessCurrent)
	assertPayload(t, env, "outcome", OutcomeObserved)
	assertPayloadKeysAbsent(t, env, "scrape_url", "labels", "discovered_labels")
	assertPayloadForbidden(t, env, "10.0.0.1", "user:pass", "checkout-abc")
	if got := fmt.Sprint(env.Payload["scrape_url_fingerprint"]); !strings.HasPrefix(got, "sha256:") {
		t.Fatalf("scrape_url_fingerprint = %q, want sha256 fingerprint", got)
	}
}

func TestObservedRuleEnvelopeDropsPromQLAndLabelValues(t *testing.T) {
	t.Parallel()

	ctx := testEnvelopeContext()
	rule := Rule{
		ProviderObjectID:   "checkout.rules:HighLatency",
		GroupName:          "checkout.rules",
		RuleName:           "HighLatency",
		RuleType:           RuleTypeAlerting,
		Health:             "ok",
		Query:              "histogram_quantile(0.95, rate(secret_bucket[5m]))",
		LabelKeys:          []string{"service", "severity"},
		AnnotationKeys:     []string{"summary"},
		QueryRedacted:      true,
		LastEvaluationAt:   time.Date(2026, 6, 1, 12, 0, 5, 0, time.UTC),
		DeclaredMatchState: MatchStateMatchedDeclared,
	}

	env, err := NewObservedRuleEnvelope(ctx, rule)
	if err != nil {
		t.Fatalf("NewObservedRuleEnvelope() error = %v, want nil", err)
	}

	assertObservabilityEnvelope(t, env, facts.ObservabilityObservedRuleFactKind)
	assertPayload(t, env, "rule_group", "checkout.rules")
	assertPayload(t, env, "rule_name", "HighLatency")
	assertPayload(t, env, "rule_type", RuleTypeAlerting)
	assertPayload(t, env, "query_redacted", true)
	assertPayloadKeysAbsent(t, env, "query", "labels", "annotations")
	assertPayloadForbidden(t, env, "histogram_quantile", "secret_bucket")
}

func TestCoverageWarningEnvelopePreservesStaleState(t *testing.T) {
	t.Parallel()

	ctx := testEnvelopeContext()
	warning := Warning{
		ResourceClass: ResourceClassRule,
		ResourceID:    "checkout.rules:HighLatency",
		Reason:        WarningStale,
	}

	env, err := NewCoverageWarningEnvelope(ctx, warning)
	if err != nil {
		t.Fatalf("NewCoverageWarningEnvelope() error = %v, want nil", err)
	}

	assertObservabilityEnvelope(t, env, facts.ObservabilityCoverageWarningFactKind)
	assertPayload(t, env, "warning_kind", WarningStale)
	assertPayload(t, env, "freshness_state", FreshnessStale)
	assertPayload(t, env, "outcome", OutcomeStale)
}

func TestCoverageWarningEnvelopeUsesTruncatedReason(t *testing.T) {
	t.Parallel()

	if WarningTruncated != "truncated" {
		t.Fatalf("WarningTruncated = %q, want %q", WarningTruncated, "truncated")
	}

	ctx := testEnvelopeContext()
	warning := Warning{
		ResourceClass: ResourceClassTarget,
		Reason:        WarningTruncated,
	}

	env, err := NewCoverageWarningEnvelope(ctx, warning)
	if err != nil {
		t.Fatalf("NewCoverageWarningEnvelope() error = %v, want nil", err)
	}

	assertObservabilityEnvelope(t, env, facts.ObservabilityCoverageWarningFactKind)
	assertPayload(t, env, "warning_kind", WarningTruncated)
	assertPayload(t, env, "resource_class", ResourceClassTarget)
}

func testEnvelopeContext() EnvelopeContext {
	return EnvelopeContext{
		ScopeID:             "prometheus:instance:prod",
		GenerationID:        "generation-2026-06-01T12:00:00Z",
		CollectorInstanceID: "collector-prometheus-prod",
		FencingToken:        42,
		ObservedAt:          time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC),
		SourceInstanceID:    "prometheus-prod",
		SourceKind:          SourceKindPrometheus,
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
