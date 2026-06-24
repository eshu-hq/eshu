// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package loki

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

func TestLiveLokiObservedLogSignalEvidence(t *testing.T) {
	if liveLokiEnvFirst("ESHU_LOKI_LIVE") != "1" {
		t.Skip("set ESHU_LOKI_LIVE=1 to run the live Loki metadata smoke")
	}

	target, secrets := liveLokiTarget(t)
	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "collector-loki-live",
		Targets:             []TargetConfig{target},
		Now:                 time.Now,
	})
	if err != nil {
		liveLokiAssertNoSecrets(t, "NewClaimedSource error", err.Error(), secrets)
		t.Fatalf("NewClaimedSource() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	collected, ok, err := source.NextClaimed(ctx, liveLokiWorkItem(target.ScopeID))
	if err != nil {
		liveLokiAssertNoSecrets(t, "NextClaimed error", err.Error(), secrets)
		t.Fatalf("NextClaimed() error = %v", err)
	}
	if !ok {
		t.Fatal("NextClaimed() ok = false, want true")
	}

	envs := collectFacts(t, collected)
	if got, want := collected.FactCount, len(envs); got != want {
		t.Fatalf("FactCount = %d, want len(envelopes) %d", got, want)
	}
	liveLokiAssertObservedEvidence(t, envs, secrets)
}

func TestLiveLokiSecretScanDetectsEnvelopeCredentials(t *testing.T) {
	t.Parallel()

	envelope := facts.Envelope{
		FactID: "safe",
		Payload: map[string]any{
			"tenant": "loki-tenant-secret",
		},
	}
	if !liveLokiContainsSecret(envelope, []string{"loki-tenant-secret"}) {
		t.Fatal("liveLokiContainsSecret() = false, want true")
	}
	if liveLokiContainsSecret(facts.Envelope{FactID: "safe"}, []string{"loki-tenant-secret"}) {
		t.Fatal("liveLokiContainsSecret() = true, want false for safe envelope")
	}
}

func liveLokiTarget(t *testing.T) (TargetConfig, []string) {
	t.Helper()

	baseURL := liveLokiRequiredEnv(t, "ESHU_LOKI_BASE_URL", "LOKI_BASE_URL", "LOKI_URL")
	token := liveLokiEnvFirst("ESHU_LOKI_API_TOKEN", "LOKI_API_TOKEN")
	tenantID := liveLokiEnvFirst("ESHU_LOKI_TENANT_ID", "LOKI_TENANT_ID")
	instanceID := liveLokiEnvFirst("ESHU_LOKI_INSTANCE_ID")
	if instanceID == "" {
		instanceID = "loki-live"
	}
	scopeID := liveLokiEnvFirst("ESHU_LOKI_SCOPE_ID")
	if scopeID == "" {
		scopeID = "loki:instance:" + instanceID
	}
	return TargetConfig{
		ScopeID:                scopeID,
		InstanceID:             instanceID,
		BaseURL:                strings.TrimRight(baseURL, "/"),
		PathPrefix:             liveLokiEnvFirst("ESHU_LOKI_PATH_PREFIX"),
		Token:                  token,
		TenantID:               tenantID,
		ResourceLimit:          liveLokiIntEnv(t, "ESHU_LOKI_RESOURCE_LIMIT", 5),
		LabelValueNames:        liveLokiCSVEnv("ESHU_LOKI_LABEL_VALUE_NAMES"),
		MaxLabelValuesPerLabel: liveLokiIntEnv(t, "ESHU_LOKI_MAX_LABEL_VALUES_PER_LABEL", 5),
		SeriesMatchers:         liveLokiCSVEnv("ESHU_LOKI_SERIES_MATCHERS"),
		StaleAfter:             liveLokiDurationEnv(t, "ESHU_LOKI_STALE_AFTER", 0),
		Enabled:                true,
	}, liveLokiSecrets(token, tenantID)
}

func liveLokiWorkItem(scopeID string) workflow.WorkItem {
	now := time.Now().UTC()
	return workflow.WorkItem{
		WorkItemID:          "loki:collector-loki-live:live-smoke",
		RunID:               "loki:collector-loki-live:live-smoke",
		CollectorKind:       scope.CollectorKind(CollectorKind),
		CollectorInstanceID: "collector-loki-live",
		SourceSystem:        CollectorKind,
		ScopeID:             scopeID,
		AcceptanceUnitID:    scopeID,
		SourceRunID:         "loki:live-smoke",
		GenerationID:        "loki:live-smoke",
		FairnessKey:         scopeID,
		Status:              workflow.WorkItemStatusPending,
		CurrentFencingToken: 1,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
}

func liveLokiAssertObservedEvidence(t *testing.T, envs []facts.Envelope, secrets []string) {
	t.Helper()

	if len(envs) == 0 {
		t.Fatal("live Loki smoke emitted no observability facts")
	}
	allowed := map[string]struct{}{
		facts.ObservabilitySourceInstanceFactKind:    {},
		facts.ObservabilityObservedLogSignalFactKind: {},
		facts.ObservabilityObservedRuleFactKind:      {},
		facts.ObservabilityCoverageWarningFactKind:   {},
	}
	counts := countByFactKind(envs)
	if got := counts[facts.ObservabilitySourceInstanceFactKind]; got != 1 {
		t.Fatalf("source_instance facts = %d, want 1; all counts %#v", got, counts)
	}
	if len(envs) == 1 && !liveLokiSourceFetched(envs[0]) {
		t.Fatal("live Loki smoke emitted only source_instance without provider fetch evidence")
	}
	for _, envelope := range envs {
		if _, ok := allowed[envelope.FactKind]; !ok {
			t.Fatalf("FactKind = %q, want Loki observability fact", envelope.FactKind)
		}
		if envelope.SchemaVersion != facts.ObservabilitySchemaVersionV1 {
			t.Fatalf("%s SchemaVersion = %q, want %q", envelope.FactKind, envelope.SchemaVersion, facts.ObservabilitySchemaVersionV1)
		}
		if envelope.SourceConfidence != facts.SourceConfidenceReported {
			t.Fatalf("%s SourceConfidence = %q, want reported", envelope.FactKind, envelope.SourceConfidence)
		}
		liveLokiAssertEnvelopeSanitized(t, envelope, secrets)
	}
}

func liveLokiSourceFetched(envelope facts.Envelope) bool {
	if envelope.FactKind != facts.ObservabilitySourceInstanceFactKind {
		return false
	}
	return liveLokiIntPayload(envelope.Payload["pages_fetched"]) > 0
}

func liveLokiIntPayload(value any) int {
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

func liveLokiRequiredEnv(t *testing.T, keys ...string) string {
	t.Helper()

	if value := liveLokiEnvFirst(keys...); value != "" {
		return value
	}
	t.Skipf("set one of %s to run the live Loki metadata smoke", strings.Join(keys, ", "))
	return ""
}

func liveLokiEnvFirst(keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}

func liveLokiCSVEnv(key string) []string {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			values = append(values, trimmed)
		}
	}
	return values
}

func liveLokiIntEnv(t *testing.T, key string, fallback int) int {
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

func liveLokiDurationEnv(t *testing.T, key string, fallback time.Duration) time.Duration {
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

func liveLokiSecrets(values ...string) []string {
	secrets := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); len(trimmed) >= 3 {
			secrets = append(secrets, trimmed)
		}
	}
	return secrets
}

func liveLokiAssertEnvelopeSanitized(t *testing.T, envelope facts.Envelope, secrets []string) {
	t.Helper()

	if strings.Contains(envelope.SourceRef.SourceURI, "?") || strings.Contains(envelope.SourceRef.SourceURI, "#") {
		t.Fatalf("SourceRef.SourceURI = %q, want no query or fragment", envelope.SourceRef.SourceURI)
	}
	liveLokiAssertNoSecrets(t, envelope.FactKind+" envelope", envelope, secrets)
}

func liveLokiAssertNoSecrets(t *testing.T, label string, value any, secrets []string) {
	t.Helper()

	if liveLokiContainsSecret(value, secrets) {
		t.Fatalf("%s retained live credential material", label)
	}
}

func liveLokiContainsSecret(value any, secrets []string) bool {
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
