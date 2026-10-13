// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package loki

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestClaimedSourceEmitsObservedLokiFacts(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	client := &stubEvidenceClient{result: CollectionResult{
		Source: SourceInstance{
			Provider:         ProviderLoki,
			SourceInstanceID: "loki-prod",
		},
		Signals: []LogSignal{{
			ProviderObjectID: "signal-1",
			SignalKind:       SignalKindLabelSet,
			LabelKeys:        []string{"app", "namespace"},
		}},
		Rules: []Rule{{
			ProviderObjectID: "prod/checkout.rules:HighLogErrors",
			Namespace:        "prod",
			GroupName:        "checkout.rules",
			RuleName:         "HighLogErrors",
			RuleType:         RuleTypeAlerting,
		}},
		Warnings: []Warning{{
			ResourceClass: ResourceClassLogSignal,
			ResourceID:    "label:trace_id",
			Reason:        WarningHighCardinality,
		}},
		ObservedAt: observedAt,
		Stats: CollectionStats{
			PagesFetched:            3,
			Redactions:              2,
			HighCardinalityRejected: 1,
			Stale:                   1,
		},
	}}
	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "collector-loki-prod",
		Targets: []TargetConfig{{
			ScopeID:       "loki:tenant:prod",
			InstanceID:    "loki-prod",
			BaseURL:       "https://loki.example.internal",
			ResourceLimit: 50,
			Enabled:       true,
		}},
		ClientFactory: func(TargetConfig) (EvidenceClient, error) { return client, nil },
		Now:           func() time.Time { return observedAt.Add(time.Minute) },
	})
	if err != nil {
		t.Fatalf("NewClaimedSource() error = %v, want nil", err)
	}

	collected, ok, err := source.NextClaimed(context.Background(), workflow.WorkItem{
		ScopeID:             "loki:tenant:prod",
		GenerationID:        "generation-1",
		CollectorKind:       scope.CollectorKind(CollectorKind),
		CollectorInstanceID: "collector-loki-prod",
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
	if got, want := collected.Scope.ScopeKind, scope.ScopeKind(ScopeKindLogSource); got != want {
		t.Fatalf("Scope.ScopeKind = %q, want %q", got, want)
	}
	if got, want := collected.Generation.FreshnessHint, "loki_observed_metadata"; got != want {
		t.Fatalf("FreshnessHint = %q, want %q", got, want)
	}
	envs := collectFacts(t, collected)
	if got, want := len(envs), 4; got != want {
		t.Fatalf("fact count = %d, want %d", got, want)
	}
	counts := countByFactKind(envs)
	if got, want := counts[facts.ObservabilitySourceInstanceFactKind], 1; got != want {
		t.Fatalf("source_instance facts = %d, want %d", got, want)
	}
	if got, want := counts[facts.ObservabilityObservedLogSignalFactKind], 1; got != want {
		t.Fatalf("observed_log_signal facts = %d, want %d", got, want)
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
		CollectorInstanceID: "collector-loki-prod",
		Targets: []TargetConfig{{
			ScopeID:    "loki:tenant:prod",
			InstanceID: "loki-prod",
			BaseURL:    "https://loki.example.internal",
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
		CollectorInstanceID: "collector-loki-prod",
		Targets: []TargetConfig{{
			ScopeID:    "loki:tenant:prod",
			InstanceID: "loki-prod",
			BaseURL:    "https://loki.example.internal",
			Enabled:    true,
		}},
		ClientFactory: func(TargetConfig) (EvidenceClient, error) {
			return &stubEvidenceClient{err: ProviderHTTPError{StatusCode: 429, Message: "rate limited"}}, nil
		},
	})
	if err != nil {
		t.Fatalf("NewClaimedSource() error = %v, want nil", err)
	}

	_, _, err = source.NextClaimed(context.Background(), workflow.WorkItem{
		ScopeID:             "loki:tenant:prod",
		GenerationID:        "generation-1",
		CollectorKind:       scope.CollectorKind(CollectorKind),
		CollectorInstanceID: "collector-loki-prod",
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

func TestClaimedSourceClassifiesTerminalFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		err          error
		wantClass    string
		wantTerminal bool
	}{
		{
			name:         "auth denied http",
			err:          ProviderHTTPError{StatusCode: http.StatusForbidden, Message: "forbidden token-secret"},
			wantClass:    FailureAuthDenied,
			wantTerminal: true,
		},
		{
			name:         "bad data api",
			err:          ProviderAPIError{Status: "error", ErrorType: "bad_data"},
			wantClass:    FailureTerminal,
			wantTerminal: true,
		},
		{
			name:      "provider api retryable",
			err:       ProviderAPIError{Status: "error", ErrorType: "timeout"},
			wantClass: FailureRetryable,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			source, err := NewClaimedSource(SourceConfig{
				CollectorInstanceID: "collector-loki-prod",
				Targets: []TargetConfig{{
					ScopeID:    "loki:tenant:prod",
					InstanceID: "loki-prod",
					BaseURL:    "https://loki.example.internal",
					Enabled:    true,
				}},
				ClientFactory: func(TargetConfig) (EvidenceClient, error) {
					return &stubEvidenceClient{err: tt.err}, nil
				},
			})
			if err != nil {
				t.Fatalf("NewClaimedSource() error = %v, want nil", err)
			}

			_, _, err = source.NextClaimed(context.Background(), workflow.WorkItem{
				ScopeID:             "loki:tenant:prod",
				GenerationID:        "generation-1",
				CollectorKind:       scope.CollectorKind(CollectorKind),
				CollectorInstanceID: "collector-loki-prod",
			})
			if err == nil {
				t.Fatal("NextClaimed() error = nil, want provider failure")
			}
			var failure ProviderFailure
			if !errors.As(err, &failure) {
				t.Fatalf("NextClaimed() error = %T, want ProviderFailure", err)
			}
			if got := failure.FailureClass(); got != tt.wantClass {
				t.Fatalf("FailureClass() = %q, want %q", got, tt.wantClass)
			}
			if got := failure.TerminalFailure(); got != tt.wantTerminal {
				t.Fatalf("TerminalFailure() = %v, want %v", got, tt.wantTerminal)
			}
			if strings.Contains(err.Error(), "token-secret") {
				t.Fatalf("provider failure leaked sensitive message: %v", err)
			}
		})
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
