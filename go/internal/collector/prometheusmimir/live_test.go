// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package prometheusmimir

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestLivePrometheusMimirObservedMetricEvidence(t *testing.T) {
	if liveMetricEnvFirst("ESHU_PROMETHEUS_MIMIR_LIVE") != "1" {
		t.Skip("set ESHU_PROMETHEUS_MIMIR_LIVE=1 to run the live Prometheus/Mimir metadata smoke")
	}

	target, secrets := liveMetricTarget(t)
	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "collector-prometheus-mimir-live",
		Targets:             []TargetConfig{target},
		Now:                 time.Now,
	})
	if err != nil {
		liveMetricAssertNoSecrets(t, "NewClaimedSource error", err.Error(), secrets)
		t.Fatalf("NewClaimedSource() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	collected, ok, err := source.NextClaimed(ctx, liveMetricWorkItem(target.ScopeID))
	if err != nil {
		liveMetricAssertNoSecrets(t, "NextClaimed error", err.Error(), secrets)
		t.Fatalf("NextClaimed() error = %v", err)
	}
	if !ok {
		t.Fatal("NextClaimed() ok = false, want true")
	}

	envs := collectFacts(t, collected)
	if got, want := collected.FactCount(), len(envs); got != want {
		t.Fatalf("FactCount = %d, want len(envelopes) %d", got, want)
	}
	liveMetricAssertObservedEvidence(t, envs, secrets)
}

func TestLiveMetricSecretScanDetectsEnvelopeCredentials(t *testing.T) {
	t.Parallel()

	envelope := facts.Envelope{
		FactID: "safe",
		Payload: map[string]any{
			"tenant": "mimir-tenant-secret",
		},
	}
	if !liveMetricContainsSecret(envelope, []string{"mimir-tenant-secret"}) {
		t.Fatal("liveMetricContainsSecret() = false, want true")
	}
	if liveMetricContainsSecret(facts.Envelope{FactID: "safe"}, []string{"mimir-tenant-secret"}) {
		t.Fatal("liveMetricContainsSecret() = true, want false for safe envelope")
	}
}

func liveMetricTarget(t *testing.T) (TargetConfig, []string) {
	t.Helper()

	provider := normalizedProvider(liveMetricEnvFirst("ESHU_PROMETHEUS_MIMIR_PROVIDER"))
	baseURL := liveMetricRequiredEnv(t, "ESHU_PROMETHEUS_MIMIR_BASE_URL", "PROMETHEUS_BASE_URL", "MIMIR_BASE_URL")
	token := liveMetricEnvFirst("ESHU_PROMETHEUS_MIMIR_API_TOKEN", "PROMETHEUS_API_TOKEN", "MIMIR_API_TOKEN")
	tenantID := liveMetricEnvFirst("ESHU_PROMETHEUS_MIMIR_TENANT_ID", "MIMIR_TENANT_ID")
	instanceID := liveMetricEnvFirst("ESHU_PROMETHEUS_MIMIR_INSTANCE_ID")
	if instanceID == "" {
		instanceID = provider + "-live"
	}
	scopeID := liveMetricEnvFirst("ESHU_PROMETHEUS_MIMIR_SCOPE_ID")
	if scopeID == "" {
		scopeID = provider + ":instance:" + instanceID
	}
	return TargetConfig{
		Provider:      provider,
		ScopeID:       scopeID,
		InstanceID:    instanceID,
		BaseURL:       strings.TrimRight(baseURL, "/"),
		PathPrefix:    liveMetricEnvFirst("ESHU_PROMETHEUS_MIMIR_PATH_PREFIX"),
		Token:         token,
		TenantID:      tenantID,
		ResourceLimit: liveMetricIntEnv(t, "ESHU_PROMETHEUS_MIMIR_RESOURCE_LIMIT", 5),
		StaleAfter:    liveMetricDurationEnv(t, "ESHU_PROMETHEUS_MIMIR_STALE_AFTER", 0),
		Enabled:       true,
	}, liveMetricSecrets(token, tenantID)
}

func liveMetricWorkItem(scopeID string) workflow.WorkItem {
	now := time.Now().UTC()
	return workflow.WorkItem{
		WorkItemID:          "prometheus-mimir:collector-prometheus-mimir-live:live-smoke",
		RunID:               "prometheus-mimir:collector-prometheus-mimir-live:live-smoke",
		CollectorKind:       scope.CollectorKind(CollectorKind),
		CollectorInstanceID: "collector-prometheus-mimir-live",
		SourceSystem:        CollectorKind,
		ScopeID:             scopeID,
		AcceptanceUnitID:    scopeID,
		SourceRunID:         "prometheus-mimir:live-smoke",
		GenerationID:        "prometheus-mimir:live-smoke",
		FairnessKey:         scopeID,
		Status:              workflow.WorkItemStatusPending,
		CurrentFencingToken: 1,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
}

func liveMetricAssertObservedEvidence(t *testing.T, envs []facts.Envelope, secrets []string) {
	t.Helper()

	if len(envs) == 0 {
		t.Fatal("live Prometheus/Mimir smoke emitted no observability facts")
	}
	allowed := map[string]struct{}{
		facts.ObservabilitySourceInstanceFactKind:  {},
		facts.ObservabilityObservedTargetFactKind:  {},
		facts.ObservabilityObservedRuleFactKind:    {},
		facts.ObservabilityCoverageWarningFactKind: {},
	}
	counts := countByFactKind(envs)
	if got := counts[facts.ObservabilitySourceInstanceFactKind]; got != 1 {
		t.Fatalf("source_instance facts = %d, want 1; all counts %#v", got, counts)
	}
	if len(envs) == 1 && !liveMetricSourceFetched(envs[0]) {
		t.Fatal("live Prometheus/Mimir smoke emitted only source_instance without provider fetch evidence")
	}
	for _, envelope := range envs {
		if _, ok := allowed[envelope.FactKind]; !ok {
			t.Fatalf("FactKind = %q, want Prometheus/Mimir observability fact", envelope.FactKind)
		}
		if envelope.SchemaVersion != facts.ObservabilitySchemaVersionV1 {
			t.Fatalf("%s SchemaVersion = %q, want %q", envelope.FactKind, envelope.SchemaVersion, facts.ObservabilitySchemaVersionV1)
		}
		if envelope.SourceConfidence != facts.SourceConfidenceReported {
			t.Fatalf("%s SourceConfidence = %q, want reported", envelope.FactKind, envelope.SourceConfidence)
		}
		liveMetricAssertEnvelopeSanitized(t, envelope, secrets)
	}
}

func liveMetricSourceFetched(envelope facts.Envelope) bool {
	if envelope.FactKind != facts.ObservabilitySourceInstanceFactKind {
		return false
	}
	return liveMetricIntPayload(envelope.Payload["pages_fetched"]) > 0
}

func liveMetricIntPayload(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	default:
		return 0
	}
}

func liveMetricRequiredEnv(t *testing.T, keys ...string) string {
	t.Helper()

	if value := liveMetricEnvFirst(keys...); value != "" {
		return value
	}
	t.Skipf("set one of %s to run the live Prometheus/Mimir metadata smoke", strings.Join(keys, ", "))
	return ""
}

func liveMetricEnvFirst(keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}

func liveMetricIntEnv(t *testing.T, key string, fallback int) int {
	t.Helper()

	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		t.Fatalf("%s=%q must be an integer", key, raw)
	}
	return value
}

func liveMetricDurationEnv(t *testing.T, key string, fallback time.Duration) time.Duration {
	t.Helper()

	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	value, err := time.ParseDuration(raw)
	if err != nil {
		t.Fatalf("%s=%q must be a Go duration", key, raw)
	}
	return value
}

func liveMetricSecrets(values ...string) []string {
	secrets := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); len(trimmed) >= 3 {
			secrets = append(secrets, trimmed)
		}
	}
	return secrets
}

func liveMetricAssertEnvelopeSanitized(t *testing.T, envelope facts.Envelope, secrets []string) {
	t.Helper()

	if strings.Contains(envelope.SourceRef.SourceURI, "?") || strings.Contains(envelope.SourceRef.SourceURI, "#") {
		t.Fatalf("SourceRef.SourceURI = %q, want no query or fragment", envelope.SourceRef.SourceURI)
	}
	liveMetricAssertNoSecrets(t, envelope.FactKind+" envelope", envelope, secrets)
}

func liveMetricAssertNoSecrets(t *testing.T, label string, value any, secrets []string) {
	t.Helper()

	if liveMetricContainsSecret(value, secrets) {
		t.Fatalf("%s retained live credential material", label)
	}
}

func liveMetricContainsSecret(value any, secrets []string) bool {
	if len(secrets) == 0 || value == nil {
		return false
	}
	text := fmt.Sprint(value)
	if encoded, err := json.Marshal(value); err == nil {
		text += string(encoded)
	}
	for _, secret := range secrets {
		if secret != "" && strings.Contains(text, secret) {
			return true
		}
	}
	return false
}
