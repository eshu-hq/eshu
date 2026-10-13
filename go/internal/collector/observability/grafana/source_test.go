// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package grafana

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestClaimedSourceEmitsObservedMetadataFacts(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	client := &stubEvidenceClient{result: CollectionResult{
		Resources: []Resource{
			{Class: ResourceClassFolder, UID: "folder-prod", Title: "Production"},
			{Class: ResourceClassDashboard, UID: "dash-checkout", FolderUID: "folder-prod", Title: "Checkout"},
			{Class: ResourceClassDatasource, UID: "prometheus-prod", Name: "Prometheus", DatasourceType: "prometheus"},
		},
		Rules: []AlertRule{{UID: "rule-checkout-latency", RuleGroup: "checkout.rules", FolderUID: "folder-prod"}},
		Warnings: []Warning{{
			ResourceClass: ResourceClassDashboard,
			ResourceID:    "dash-manual",
			Reason:        WarningManualProviderResource,
		}},
		ObservedAt: observedAt,
		Stats: CollectionStats{
			PagesFetched: 3,
			Redactions:   2,
		},
	}}
	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "collector-grafana-prod",
		Targets: []TargetConfig{{
			Provider:      ProviderGrafana,
			ScopeID:       "grafana:instance:prod",
			InstanceID:    "grafana-prod",
			BaseURL:       "https://grafana.example.internal",
			Token:         "grafana-token",
			ResourceLimit: 25,
			Enabled:       true,
		}},
		ClientFactory: func(TargetConfig) (EvidenceClient, error) { return client, nil },
		Now:           func() time.Time { return observedAt.Add(time.Minute) },
	})
	if err != nil {
		t.Fatalf("NewClaimedSource() error = %v, want nil", err)
	}

	collected, ok, err := source.NextClaimed(context.Background(), workflow.WorkItem{
		ScopeID:             "grafana:instance:prod",
		GenerationID:        "generation-1",
		CollectorKind:       scope.CollectorKind(CollectorKind),
		CollectorInstanceID: "collector-grafana-prod",
		CurrentFencingToken: 7,
	})
	if err != nil {
		t.Fatalf("NextClaimed() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("NextClaimed() ok = false, want true")
	}
	if got, want := collected.Scope.CollectorKind, scope.CollectorKind(CollectorKind); got != want {
		t.Fatalf("Scope.CollectorKind = %q, want %q", got, want)
	}
	if got, want := collected.Scope.ScopeKind, scope.ScopeKind(ScopeKindGrafanaInstance); got != want {
		t.Fatalf("Scope.ScopeKind = %q, want %q", got, want)
	}
	if got, want := collected.Generation.FreshnessHint, "grafana_observed_metadata"; got != want {
		t.Fatalf("FreshnessHint = %q, want %q", got, want)
	}
	envs := collectFacts(t, collected)
	if got, want := len(envs), 6; got != want {
		t.Fatalf("fact count = %d, want %d", got, want)
	}
	counts := countByFactKind(envs)
	if got, want := counts[facts.ObservabilitySourceInstanceFactKind], 1; got != want {
		t.Fatalf("source_instance facts = %d, want %d", got, want)
	}
	if got, want := counts[facts.ObservabilityObservedDashboardFactKind], 3; got != want {
		t.Fatalf("observed_dashboard facts = %d, want %d", got, want)
	}
	if got, want := counts[facts.ObservabilityObservedRuleFactKind], 1; got != want {
		t.Fatalf("observed_rule facts = %d, want %d", got, want)
	}
	if got, want := counts[facts.ObservabilityCoverageWarningFactKind], 1; got != want {
		t.Fatalf("coverage_warning facts = %d, want %d", got, want)
	}
	assertStableKeysUnique(t, envs)
}

func TestClaimedSourceSkipsDisabledTargets(t *testing.T) {
	t.Parallel()

	_, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "collector-grafana-prod",
		Targets: []TargetConfig{{
			Provider:   ProviderGrafana,
			ScopeID:    "grafana:instance:prod",
			InstanceID: "grafana-prod",
			BaseURL:    "https://grafana.example.internal",
			Token:      "grafana-token",
			Enabled:    false,
		}},
		ClientFactory: func(TargetConfig) (EvidenceClient, error) {
			t.Fatal("disabled target should not construct a client")
			return nil, nil
		},
	})
	if err == nil {
		t.Fatal("NewClaimedSource() error = nil, want no enabled target error")
	}
}

func TestClaimedSourceClassifiesRateLimitFailures(t *testing.T) {
	t.Parallel()

	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "collector-grafana-prod",
		Targets: []TargetConfig{{
			Provider:   ProviderGrafana,
			ScopeID:    "grafana:instance:prod",
			InstanceID: "grafana-prod",
			BaseURL:    "https://grafana.example.internal",
			Token:      "grafana-token",
			Enabled:    true,
		}},
		ClientFactory: func(TargetConfig) (EvidenceClient, error) {
			return &stubEvidenceClient{err: GrafanaError{StatusCode: 429, Message: "rate limited"}}, nil
		},
	})
	if err != nil {
		t.Fatalf("NewClaimedSource() error = %v, want nil", err)
	}

	_, _, err = source.NextClaimed(context.Background(), workflow.WorkItem{
		ScopeID:             "grafana:instance:prod",
		GenerationID:        "generation-1",
		CollectorKind:       scope.CollectorKind(CollectorKind),
		CollectorInstanceID: "collector-grafana-prod",
	})
	if err == nil {
		t.Fatal("NextClaimed() error = nil, want provider failure")
	}
	var failure ProviderFailure
	if !errors.As(err, &failure) {
		t.Fatalf("NextClaimed() error = %T, want ProviderFailure", err)
	}
	if got, want := failure.FailureClass(), FailureRateLimited; got != want {
		t.Fatalf("FailureClass() = %q, want %q", got, want)
	}
}

type stubEvidenceClient struct {
	result CollectionResult
	err    error
}

func (c *stubEvidenceClient) CollectObservedMetadata(context.Context, TargetConfig) (CollectionResult, error) {
	return c.result, c.err
}

func collectFacts(t *testing.T, collected collector.CollectedGeneration) []facts.Envelope {
	t.Helper()
	var envs []facts.Envelope
	for env := range collected.Facts {
		envs = append(envs, env)
	}
	return envs
}

func countByFactKind(envs []facts.Envelope) map[string]int {
	out := map[string]int{}
	for _, env := range envs {
		out[env.FactKind]++
	}
	return out
}

func assertStableKeysUnique(t *testing.T, envs []facts.Envelope) {
	t.Helper()
	seen := map[string]struct{}{}
	for _, env := range envs {
		if env.StableFactKey == "" {
			t.Fatalf("%s StableFactKey is blank", env.FactKind)
		}
		if _, exists := seen[env.StableFactKey]; exists {
			t.Fatalf("duplicate stable fact key %q", env.StableFactKey)
		}
		seen[env.StableFactKey] = struct{}{}
	}
}
