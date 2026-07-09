// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package pagerduty

import (
	"context"
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestLivePagerDutyIncidentOrConfigEvidence(t *testing.T) {
	if os.Getenv("ESHU_PAGERDUTY_LIVE") != "1" {
		t.Skip("set ESHU_PAGERDUTY_LIVE=1 to run the live PagerDuty collector smoke")
	}

	token, tokenEnv := livePagerDutyToken(t)
	accountID := firstLiveValue("ESHU_PAGERDUTY_ACCOUNT_ID", "live-account")
	scopeID := firstLiveValue("ESHU_PAGERDUTY_SCOPE_ID", "pagerduty:account:"+accountID)
	configValidation := liveBoolValue(t, "ESHU_PAGERDUTY_CONFIG_VALIDATION_ENABLED", true)
	target := TargetConfig{
		Provider:                ProviderPagerDuty,
		ScopeID:                 scopeID,
		AccountID:               accountID,
		Token:                   token,
		APIBaseURL:              os.Getenv("ESHU_PAGERDUTY_API_BASE_URL"),
		SourceURI:               firstLiveValue("ESHU_PAGERDUTY_SOURCE_URI", "pagerduty://"+accountID),
		IncidentLimit:           liveIntValue(t, "ESHU_PAGERDUTY_INCIDENT_LIMIT", 1),
		IncidentLookback:        liveDurationValue(t, "ESHU_PAGERDUTY_INCIDENT_LOOKBACK", 24*time.Hour),
		LogEntryLimit:           liveIntValue(t, "ESHU_PAGERDUTY_LOG_ENTRY_LIMIT", 1),
		ChangeEventLimit:        liveIntValue(t, "ESHU_PAGERDUTY_CHANGE_EVENT_LIMIT", 1),
		AllowedServiceIDs:       liveCSVValue(os.Getenv("ESHU_PAGERDUTY_ALLOWED_SERVICE_IDS")),
		ConfigValidationEnabled: configValidation,
		ConfigResourceLimit:     liveIntValue(t, "ESHU_PAGERDUTY_CONFIG_RESOURCE_LIMIT", 2),
	}

	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "pagerduty-primary",
		Targets:             []TargetConfig{target},
	})
	if err != nil {
		t.Fatalf("NewClaimedSource() error = %v, want nil", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	collected, ok, err := source.NextClaimed(ctx, livePagerDutyWorkItem(scopeID))
	if err != nil {
		t.Fatalf("NextClaimed() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("NextClaimed() ok = false, want true")
	}

	envs := drainFacts(collected.Facts)
	if got, want := collected.FactCount(), len(envs); got != want {
		t.Fatalf("FactCount = %d, want len(envelopes) %d", got, want)
	}
	if len(envs) == 0 {
		t.Fatalf("live PagerDuty smoke emitted no facts; token env %s resolved but no incident or config evidence was visible", tokenEnv)
	}
	assertLivePagerDutyFactKinds(t, envs)
	assertLivePagerDutyTokenNotRetained(t, token, envs)
}

func livePagerDutyToken(t *testing.T) (string, string) {
	t.Helper()
	for _, name := range []string{"ESHU_PAGERDUTY_API_TOKEN", "PAGERDUTY_API_TOKEN", "PAGERDUTY_USER_API_KEY"} {
		if token := strings.TrimSpace(os.Getenv(name)); token != "" {
			return token, name
		}
	}
	t.Skip("set ESHU_PAGERDUTY_API_TOKEN, PAGERDUTY_API_TOKEN, or PAGERDUTY_USER_API_KEY to run the live PagerDuty collector smoke")
	return "", ""
}

func firstLiveValue(name string, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func liveCSVValue(raw string) []string {
	parts := strings.Split(raw, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			values = append(values, trimmed)
		}
	}
	return values
}

func liveIntValue(t *testing.T, name string, fallback int) int {
	t.Helper()
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		t.Fatalf("%s=%q must be an integer", name, raw)
	}
	return value
}

func liveBoolValue(t *testing.T, name string, fallback bool) bool {
	t.Helper()
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		t.Fatalf("%s=%q must be a boolean", name, raw)
	}
	return value
}

func liveDurationValue(t *testing.T, name string, fallback time.Duration) time.Duration {
	t.Helper()
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	value, err := time.ParseDuration(raw)
	if err != nil {
		t.Fatalf("%s=%q must be a Go duration", name, raw)
	}
	return value
}

func livePagerDutyWorkItem(scopeID string) workflow.WorkItem {
	now := time.Now().UTC()
	return workflow.WorkItem{
		WorkItemID:          "pagerduty:pagerduty-primary:live-smoke",
		RunID:               "pagerduty:pagerduty-primary:live-smoke",
		CollectorKind:       scope.CollectorPagerDuty,
		CollectorInstanceID: "pagerduty-primary",
		SourceSystem:        string(scope.CollectorPagerDuty),
		ScopeID:             scopeID,
		AcceptanceUnitID:    scopeID,
		SourceRunID:         "pagerduty:live-smoke",
		GenerationID:        "pagerduty:live-smoke",
		FairnessKey:         scopeID,
		Status:              workflow.WorkItemStatusPending,
		CurrentFencingToken: 1,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
}

func assertLivePagerDutyFactKinds(t *testing.T, envs []facts.Envelope) {
	t.Helper()
	allowed := map[string]struct{}{
		facts.IncidentRecordFactKind:                              {},
		facts.IncidentLifecycleEventFactKind:                      {},
		facts.ChangeRecordFactKind:                                {},
		facts.IncidentRoutingObservedPagerDutyServiceFactKind:     {},
		facts.IncidentRoutingObservedPagerDutyIntegrationFactKind: {},
		facts.IncidentRoutingCoverageWarningFactKind:              {},
	}
	for _, env := range envs {
		if _, ok := allowed[env.FactKind]; !ok {
			t.Fatalf("FactKind = %q, want PagerDuty incident or routing source fact", env.FactKind)
		}
	}
}

func assertLivePagerDutyTokenNotRetained(t *testing.T, token string, envs []facts.Envelope) {
	t.Helper()
	if token == "" {
		return
	}
	encoded, err := json.Marshal(envs)
	if err != nil {
		t.Fatalf("marshal envelopes: %v", err)
	}
	if strings.Contains(string(encoded), token) {
		t.Fatal("live PagerDuty envelopes retained the API token")
	}
}
