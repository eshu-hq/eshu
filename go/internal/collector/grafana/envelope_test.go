// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package grafana

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestObservedResourceEnvelopeRedactsSensitiveGrafanaFields(t *testing.T) {
	t.Parallel()

	ctx := testEnvelopeContext()
	dashboard := Resource{
		Class:           ResourceClassDashboard,
		UID:             "dash-checkout",
		Title:           "Checkout Latency",
		FolderUID:       "folder-prod",
		URL:             "https://grafana.example.internal/d/dash-checkout?token=secret",
		UpdatedAt:       time.Date(2026, 6, 1, 12, 5, 0, 0, time.UTC),
		ManuallyCreated: true,
		DriftReason:     DriftReasonManualProviderResource,
	}

	env, err := NewObservedResourceEnvelope(ctx, dashboard)
	if err != nil {
		t.Fatalf("NewObservedResourceEnvelope() error = %v, want nil", err)
	}

	assertObservabilityEnvelope(t, env, facts.ObservabilityObservedDashboardFactKind)
	assertPayload(t, env, "source_class", SourceClassObserved)
	assertPayload(t, env, "source_kind", SourceKindGrafana)
	assertPayload(t, env, "resource_class", ResourceClassDashboard)
	assertPayload(t, env, "provider_object_uid", "dash-checkout")
	assertPayload(t, env, "folder_uid", "folder-prod")
	assertPayload(t, env, "title_present", true)
	assertPayload(t, env, "url_present", true)
	assertPayload(t, env, "url_redacted", true)
	assertPayload(t, env, "manually_created", true)
	assertPayload(t, env, "drift_candidate_reason", DriftReasonManualProviderResource)
	assertPayload(t, env, "redaction_state", "applied")
	assertPayload(t, env, "freshness_state", FreshnessCurrent)
	assertPayload(t, env, "outcome", OutcomeObserved)
	assertPayloadKeysAbsent(t, env, "url")
	assertPayloadForbidden(t, env, "Checkout Latency", "grafana.example.internal", "token=secret")
	if got := fmt.Sprint(env.Payload["title_fingerprint"]); !strings.HasPrefix(got, "sha256:") {
		t.Fatalf("title_fingerprint = %q, want sha256 fingerprint", got)
	}
	if got := fmt.Sprint(env.Payload["url_fingerprint"]); !strings.HasPrefix(got, "sha256:") {
		t.Fatalf("url_fingerprint = %q, want sha256 fingerprint", got)
	}
	if !strings.Contains(env.StableFactKey, ctx.GenerationID) {
		t.Fatalf("StableFactKey = %q, want generation-scoped key", env.StableFactKey)
	}
}

func TestObservedRuleEnvelopeDropsQueryModelAndContactFields(t *testing.T) {
	t.Parallel()

	ctx := testEnvelopeContext()
	rule := AlertRule{
		UID:             "rule-checkout-latency",
		Title:           "Checkout Latency",
		RuleGroup:       "checkout.rules",
		FolderUID:       "folder-prod",
		DatasourceUID:   "prometheus-prod",
		Condition:       "B",
		Model:           map[string]any{"expr": "histogram_quantile(0.95, rate(secret_bucket[5m]))"},
		ContactPoint:    "oncall@example.com",
		NotificationURL: "https://hooks.example.internal/secret",
	}

	env, err := NewObservedRuleEnvelope(ctx, rule)
	if err != nil {
		t.Fatalf("NewObservedRuleEnvelope() error = %v, want nil", err)
	}

	assertObservabilityEnvelope(t, env, facts.ObservabilityObservedRuleFactKind)
	assertPayload(t, env, "alert_rule_uid", "rule-checkout-latency")
	assertPayload(t, env, "rule_group", "checkout.rules")
	assertPayload(t, env, "folder_uid", "folder-prod")
	assertPayload(t, env, "datasource_uid", "prometheus-prod")
	assertPayload(t, env, "query_model_redacted", true)
	assertPayload(t, env, "contact_point_redacted", true)
	assertPayload(t, env, "notification_url_redacted", true)
	assertPayloadKeysAbsent(t, env, "model", "contact_point", "notification_url")
	assertPayloadForbidden(t, env, "Checkout Latency", "histogram_quantile", "secret_bucket", "oncall@example.com", "hooks.example.internal")
}

func TestObservedRuleEnvelopePreservesStaleState(t *testing.T) {
	t.Parallel()

	ctx := testEnvelopeContext()
	rule := AlertRule{
		UID:            "rule-stale",
		Title:          "Stale Alert",
		RuleGroup:      "checkout.rules",
		FreshnessState: FreshnessStale,
		Outcome:        OutcomeStale,
	}

	env, err := NewObservedRuleEnvelope(ctx, rule)
	if err != nil {
		t.Fatalf("NewObservedRuleEnvelope() error = %v, want nil", err)
	}

	assertObservabilityEnvelope(t, env, facts.ObservabilityObservedRuleFactKind)
	assertPayload(t, env, "freshness_state", FreshnessStale)
	assertPayload(t, env, "outcome", OutcomeStale)
}

func TestCoverageWarningEnvelopeUsesBoundedIdentity(t *testing.T) {
	t.Parallel()

	ctx := testEnvelopeContext()
	warning := Warning{
		ResourceClass: ResourceClassAlertRule,
		ResourceID:    "rule-checkout-latency",
		Reason:        WarningPermissionHidden,
	}

	env, err := NewCoverageWarningEnvelope(ctx, warning)
	if err != nil {
		t.Fatalf("NewCoverageWarningEnvelope() error = %v, want nil", err)
	}

	assertObservabilityEnvelope(t, env, facts.ObservabilityCoverageWarningFactKind)
	assertPayload(t, env, "warning_kind", WarningPermissionHidden)
	assertPayload(t, env, "resource_class", ResourceClassAlertRule)
	assertPayload(t, env, "provider_object_uid", "rule-checkout-latency")
	assertPayload(t, env, "outcome", OutcomePermissionHidden)
	assertPayload(t, env, "redaction_state", "none")
}

func TestCoverageWarningEnvelopeUsesTruncatedReason(t *testing.T) {
	t.Parallel()

	if WarningTruncated != "truncated" {
		t.Fatalf("WarningTruncated = %q, want %q", WarningTruncated, "truncated")
	}

	ctx := testEnvelopeContext()
	warning := Warning{
		ResourceClass: ResourceClassDatasource,
		Reason:        WarningTruncated,
	}

	env, err := NewCoverageWarningEnvelope(ctx, warning)
	if err != nil {
		t.Fatalf("NewCoverageWarningEnvelope() error = %v, want nil", err)
	}

	assertObservabilityEnvelope(t, env, facts.ObservabilityCoverageWarningFactKind)
	assertPayload(t, env, "warning_kind", WarningTruncated)
	assertPayload(t, env, "resource_class", ResourceClassDatasource)
}

func testEnvelopeContext() EnvelopeContext {
	return EnvelopeContext{
		ScopeID:             "grafana:instance:prod",
		GenerationID:        "generation-2026-06-01T12:00:00Z",
		CollectorInstanceID: "collector-grafana-prod",
		FencingToken:        42,
		ObservedAt:          time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC),
		SourceURI:           "https://grafana.example.internal",
		SourceInstanceID:    "grafana-prod",
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
