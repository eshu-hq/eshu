// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package tempo

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestLiveTempoObservedTraceSignalEvidence(t *testing.T) {
	if liveTempoEnvFirst("ESHU_TEMPO_LIVE") != "1" {
		t.Skip("set ESHU_TEMPO_LIVE=1 to run the live Tempo metadata smoke")
	}

	target, secrets := liveTempoTarget(t)
	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "collector-tempo-live",
		Targets:             []TargetConfig{target},
		Now:                 time.Now,
	})
	if err != nil {
		liveTempoAssertNoSecrets(t, "NewClaimedSource error", err.Error(), secrets)
		t.Fatalf("NewClaimedSource() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	collected, ok, err := source.NextClaimed(ctx, liveTempoWorkItem(target.ScopeID))
	if err != nil {
		liveTempoAssertNoSecrets(t, "NextClaimed error", err.Error(), secrets)
		t.Fatalf("NextClaimed() error = %v", err)
	}
	if !ok {
		t.Fatal("NextClaimed() ok = false, want true")
	}

	envs := liveTempoCollectFacts(t, collected)
	if got, want := collected.FactCount, len(envs); got != want {
		t.Fatalf("FactCount = %d, want len(envelopes) %d", got, want)
	}
	liveTempoAssertObservedEvidence(t, envs, secrets)
}

func TestLiveTempoSecretScanDetectsEnvelopeCredentials(t *testing.T) {
	t.Parallel()

	envelope := facts.Envelope{
		FactID: "safe",
		Payload: map[string]any{
			"tenant": "tempo-tenant-secret",
		},
	}
	if !liveTempoContainsSecret(envelope, []string{"tempo-tenant-secret"}) {
		t.Fatal("liveTempoContainsSecret() = false, want true")
	}
	if liveTempoContainsSecret(facts.Envelope{FactID: "safe"}, []string{"tempo-tenant-secret"}) {
		t.Fatal("liveTempoContainsSecret() = true, want false for safe envelope")
	}
}

func liveTempoTarget(t *testing.T) (TargetConfig, []string) {
	t.Helper()

	baseURL := liveTempoRequiredEnv(t, "ESHU_TEMPO_BASE_URL", "TEMPO_BASE_URL", "TEMPO_URL")
	token := liveTempoEnvFirst("ESHU_TEMPO_API_TOKEN", "TEMPO_API_TOKEN")
	tenantID := liveTempoEnvFirst("ESHU_TEMPO_TENANT_ID", "TEMPO_TENANT_ID")
	instanceID := liveTempoEnvFirst("ESHU_TEMPO_INSTANCE_ID")
	if instanceID == "" {
		instanceID = "tempo-live"
	}
	scopeID := liveTempoEnvFirst("ESHU_TEMPO_SCOPE_ID")
	if scopeID == "" {
		scopeID = "tempo:instance:" + instanceID
	}
	return TargetConfig{
		ScopeID:              scopeID,
		InstanceID:           instanceID,
		BaseURL:              strings.TrimRight(baseURL, "/"),
		PathPrefix:           liveTempoEnvFirst("ESHU_TEMPO_PATH_PREFIX"),
		Token:                token,
		TenantID:             tenantID,
		ResourceLimit:        liveTempoIntEnv(t, "ESHU_TEMPO_RESOURCE_LIMIT", 5),
		TagValueNames:        liveTempoCSVEnv("ESHU_TEMPO_TAG_VALUE_NAMES"),
		MaxTagValuesPerTag:   liveTempoIntEnv(t, "ESHU_TEMPO_MAX_TAG_VALUES_PER_TAG", 5),
		StaleAfter:           liveTempoDurationEnv(t, "ESHU_TEMPO_STALE_AFTER", 0),
		FreshnessProbeEnable: liveTempoBoolEnv(t, "ESHU_TEMPO_FRESHNESS_PROBE", false),
		Lookback:             liveTempoDurationEnv(t, "ESHU_TEMPO_LOOKBACK", time.Hour),
		Enabled:              true,
	}, liveTempoSecrets(token, tenantID)
}

func liveTempoWorkItem(scopeID string) workflow.WorkItem {
	now := time.Now().UTC()
	return workflow.WorkItem{
		WorkItemID:          "tempo:collector-tempo-live:live-smoke",
		RunID:               "tempo:collector-tempo-live:live-smoke",
		CollectorKind:       scope.CollectorKind(CollectorKind),
		CollectorInstanceID: "collector-tempo-live",
		SourceSystem:        CollectorKind,
		ScopeID:             scopeID,
		AcceptanceUnitID:    scopeID,
		SourceRunID:         "tempo:live-smoke",
		GenerationID:        "tempo:live-smoke",
		FairnessKey:         scopeID,
		Status:              workflow.WorkItemStatusPending,
		CurrentFencingToken: 1,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
}

func liveTempoAssertObservedEvidence(t *testing.T, envs []facts.Envelope, secrets []string) {
	t.Helper()

	if len(envs) == 0 {
		t.Fatal("live Tempo smoke emitted no observability facts")
	}
	allowed := map[string]struct{}{
		facts.ObservabilitySourceInstanceFactKind:      {},
		facts.ObservabilityObservedTraceSignalFactKind: {},
		facts.ObservabilityCoverageWarningFactKind:     {},
	}
	counts := liveTempoCountByFactKind(envs)
	if got := counts[facts.ObservabilitySourceInstanceFactKind]; got != 1 {
		t.Fatalf("source_instance facts = %d, want 1; all counts %#v", got, counts)
	}
	if len(envs) == 1 && !liveTempoSourceFetched(envs[0]) {
		t.Fatal("live Tempo smoke emitted only source_instance without provider fetch evidence")
	}
	for _, envelope := range envs {
		if _, ok := allowed[envelope.FactKind]; !ok {
			t.Fatalf("FactKind = %q, want Tempo observability fact", envelope.FactKind)
		}
		if envelope.SchemaVersion != facts.ObservabilitySchemaVersionV1 {
			t.Fatalf("%s SchemaVersion = %q, want %q", envelope.FactKind, envelope.SchemaVersion, facts.ObservabilitySchemaVersionV1)
		}
		if envelope.SourceConfidence != facts.SourceConfidenceReported {
			t.Fatalf("%s SourceConfidence = %q, want reported", envelope.FactKind, envelope.SourceConfidence)
		}
		liveTempoAssertEnvelopeSanitized(t, envelope, secrets)
	}
}

func liveTempoSourceFetched(envelope facts.Envelope) bool {
	if envelope.FactKind != facts.ObservabilitySourceInstanceFactKind {
		return false
	}
	return liveTempoIntPayload(envelope.Payload["pages_fetched"]) > 0
}

func liveTempoCollectFacts(t *testing.T, collected collector.CollectedGeneration) []facts.Envelope {
	t.Helper()

	envs := make([]facts.Envelope, 0, collected.FactCount)
	for envelope := range collected.Facts {
		envs = append(envs, envelope)
	}
	return envs
}

func liveTempoCountByFactKind(envs []facts.Envelope) map[string]int {
	out := map[string]int{}
	for _, env := range envs {
		out[env.FactKind]++
	}
	return out
}

func liveTempoIntPayload(value any) int {
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

func liveTempoRequiredEnv(t *testing.T, keys ...string) string {
	t.Helper()

	if value := liveTempoEnvFirst(keys...); value != "" {
		return value
	}
	t.Skipf("set one of %s to run the live Tempo metadata smoke", strings.Join(keys, ", "))
	return ""
}

func liveTempoEnvFirst(keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}

func liveTempoCSVEnv(key string) []string {
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

func liveTempoIntEnv(t *testing.T, key string, fallback int) int {
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

func liveTempoBoolEnv(t *testing.T, key string, fallback bool) bool {
	t.Helper()

	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		t.Fatalf("%s=%q must be a boolean", key, raw)
	}
	return value
}

func liveTempoDurationEnv(t *testing.T, key string, fallback time.Duration) time.Duration {
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

func liveTempoSecrets(values ...string) []string {
	secrets := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); len(trimmed) >= 3 {
			secrets = append(secrets, trimmed)
		}
	}
	return secrets
}

func liveTempoAssertEnvelopeSanitized(t *testing.T, envelope facts.Envelope, secrets []string) {
	t.Helper()

	if strings.Contains(envelope.SourceRef.SourceURI, "?") || strings.Contains(envelope.SourceRef.SourceURI, "#") {
		t.Fatalf("SourceRef.SourceURI = %q, want no query or fragment", envelope.SourceRef.SourceURI)
	}
	liveTempoAssertNoSecrets(t, envelope.FactKind+" envelope", envelope, secrets)
}

func liveTempoAssertNoSecrets(t *testing.T, label string, value any, secrets []string) {
	t.Helper()

	if liveTempoContainsSecret(value, secrets) {
		t.Fatalf("%s retained live credential material", label)
	}
}

func liveTempoContainsSecret(value any, secrets []string) bool {
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
