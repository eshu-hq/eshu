// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package grafana

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

func TestLiveGrafanaObservedMetadataEvidence(t *testing.T) {
	if liveGrafanaEnvFirst("ESHU_GRAFANA_LIVE") != "1" {
		t.Skip("set ESHU_GRAFANA_LIVE=1 to run the live Grafana metadata smoke")
	}

	target, secrets := liveGrafanaTarget(t)
	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "collector-grafana-live",
		Targets:             []TargetConfig{target},
		Now:                 time.Now,
	})
	if err != nil {
		liveGrafanaAssertNoSecrets(t, "NewClaimedSource error", err.Error(), secrets)
		t.Fatalf("NewClaimedSource() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	collected, ok, err := source.NextClaimed(ctx, liveGrafanaWorkItem(target.ScopeID))
	if err != nil {
		liveGrafanaAssertNoSecrets(t, "NextClaimed error", err.Error(), secrets)
		t.Fatalf("NextClaimed() error = %v", err)
	}
	if !ok {
		t.Fatal("NextClaimed() ok = false, want true")
	}

	envs := collectFacts(t, collected)
	if got, want := collected.FactCount, len(envs); got != want {
		t.Fatalf("FactCount = %d, want len(envelopes) %d", got, want)
	}
	liveGrafanaAssertObservedEvidence(t, envs, secrets)
}

func TestLiveGrafanaSecretScanDetectsEnvelopeCredentials(t *testing.T) {
	t.Parallel()

	envelope := facts.Envelope{
		FactID: "safe",
		Payload: map[string]any{
			"authorization": "Bearer grafana-secret-token",
		},
	}
	if !liveGrafanaContainsSecret(envelope, []string{"grafana-secret-token"}) {
		t.Fatal("liveGrafanaContainsSecret() = false, want true")
	}
	if liveGrafanaContainsSecret(facts.Envelope{FactID: "safe"}, []string{"grafana-secret-token"}) {
		t.Fatal("liveGrafanaContainsSecret() = true, want false for safe envelope")
	}
}

func liveGrafanaTarget(t *testing.T) (TargetConfig, []string) {
	t.Helper()

	baseURL := liveGrafanaRequiredEnv(t, "ESHU_GRAFANA_BASE_URL", "GRAFANA_BASE_URL", "GRAFANA_URL")
	token := liveGrafanaRequiredEnv(t, "ESHU_GRAFANA_API_TOKEN", "GRAFANA_API_TOKEN")
	instanceID := liveGrafanaEnvFirst("ESHU_GRAFANA_INSTANCE_ID")
	if instanceID == "" {
		instanceID = "grafana-live"
	}
	scopeID := liveGrafanaEnvFirst("ESHU_GRAFANA_SCOPE_ID")
	if scopeID == "" {
		scopeID = "grafana:instance:" + instanceID
	}
	return TargetConfig{
		Provider:      ProviderGrafana,
		ScopeID:       scopeID,
		InstanceID:    instanceID,
		BaseURL:       strings.TrimRight(baseURL, "/"),
		Token:         token,
		ResourceLimit: liveGrafanaIntEnv(t, "ESHU_GRAFANA_RESOURCE_LIMIT", 5),
		StaleAfter:    liveGrafanaDurationEnv(t, "ESHU_GRAFANA_STALE_AFTER", 0),
		Enabled:       true,
	}, liveGrafanaSecrets(token)
}

func liveGrafanaWorkItem(scopeID string) workflow.WorkItem {
	now := time.Now().UTC()
	return workflow.WorkItem{
		WorkItemID:          "grafana:collector-grafana-live:live-smoke",
		RunID:               "grafana:collector-grafana-live:live-smoke",
		CollectorKind:       scope.CollectorKind(CollectorKind),
		CollectorInstanceID: "collector-grafana-live",
		SourceSystem:        CollectorKind,
		ScopeID:             scopeID,
		AcceptanceUnitID:    scopeID,
		SourceRunID:         "grafana:live-smoke",
		GenerationID:        "grafana:live-smoke",
		FairnessKey:         scopeID,
		Status:              workflow.WorkItemStatusPending,
		CurrentFencingToken: 1,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
}

func liveGrafanaAssertObservedEvidence(t *testing.T, envs []facts.Envelope, secrets []string) {
	t.Helper()

	if len(envs) == 0 {
		t.Fatal("live Grafana smoke emitted no observability facts")
	}
	allowed := map[string]struct{}{
		facts.ObservabilitySourceInstanceFactKind:    {},
		facts.ObservabilityObservedDashboardFactKind: {},
		facts.ObservabilityObservedRuleFactKind:      {},
		facts.ObservabilityCoverageWarningFactKind:   {},
	}
	counts := countByFactKind(envs)
	if got := counts[facts.ObservabilitySourceInstanceFactKind]; got != 1 {
		t.Fatalf("source_instance facts = %d, want 1; all counts %#v", got, counts)
	}
	if len(envs) == 1 && !liveGrafanaSourceFetched(envs[0]) {
		t.Fatal("live Grafana smoke emitted only source_instance without provider fetch evidence")
	}
	for _, envelope := range envs {
		if _, ok := allowed[envelope.FactKind]; !ok {
			t.Fatalf("FactKind = %q, want Grafana observability fact", envelope.FactKind)
		}
		if envelope.SchemaVersion != facts.ObservabilitySchemaVersionV1 {
			t.Fatalf("%s SchemaVersion = %q, want %q", envelope.FactKind, envelope.SchemaVersion, facts.ObservabilitySchemaVersionV1)
		}
		if envelope.SourceConfidence != facts.SourceConfidenceReported {
			t.Fatalf("%s SourceConfidence = %q, want reported", envelope.FactKind, envelope.SourceConfidence)
		}
		liveGrafanaAssertEnvelopeSanitized(t, envelope, secrets)
	}
}

func liveGrafanaSourceFetched(envelope facts.Envelope) bool {
	if envelope.FactKind != facts.ObservabilitySourceInstanceFactKind {
		return false
	}
	return liveGrafanaIntPayload(envelope.Payload["pages_fetched"]) > 0
}

func liveGrafanaIntPayload(value any) int {
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

func liveGrafanaRequiredEnv(t *testing.T, keys ...string) string {
	t.Helper()

	if value := liveGrafanaEnvFirst(keys...); value != "" {
		return value
	}
	t.Skipf("set one of %s to run the live Grafana metadata smoke", strings.Join(keys, ", "))
	return ""
}

func liveGrafanaEnvFirst(keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}

func liveGrafanaIntEnv(t *testing.T, key string, fallback int) int {
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

func liveGrafanaDurationEnv(t *testing.T, key string, fallback time.Duration) time.Duration {
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

func liveGrafanaSecrets(values ...string) []string {
	secrets := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); len(trimmed) >= 3 {
			secrets = append(secrets, trimmed)
		}
	}
	return secrets
}

func liveGrafanaAssertEnvelopeSanitized(t *testing.T, envelope facts.Envelope, secrets []string) {
	t.Helper()

	if strings.Contains(envelope.SourceRef.SourceURI, "?") || strings.Contains(envelope.SourceRef.SourceURI, "#") {
		t.Fatalf("SourceRef.SourceURI = %q, want no query or fragment", envelope.SourceRef.SourceURI)
	}
	liveGrafanaAssertNoSecrets(t, envelope.FactKind+" envelope", envelope, secrets)
}

func liveGrafanaAssertNoSecrets(t *testing.T, label string, value any, secrets []string) {
	t.Helper()

	if liveGrafanaContainsSecret(value, secrets) {
		t.Fatalf("%s retained live credential material", label)
	}
}

func liveGrafanaContainsSecret(value any, secrets []string) bool {
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
