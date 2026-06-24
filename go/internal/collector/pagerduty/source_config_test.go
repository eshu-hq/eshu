// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package pagerduty

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestClaimedSourceEmitsObservedPagerDutyConfigFactsWhenEnabled(t *testing.T) {
	t.Parallel()

	now := testObservedAt()
	client := staticConfigEvidenceClient{
		result: CollectionResult{ObservedAt: now},
		configResult: ConfigCollectionResult{
			Services: []ConfigService{{
				ID:          "SVC1",
				Summary:     "checkout-api",
				Status:      "active",
				UpdatedAt:   now,
				MatchState:  ConfigMatchStateNotCompared,
				DriftReason: "manually_created",
			}},
			Integrations: []ConfigIntegration{{
				ID:              "INT1",
				ServiceID:       "SVC1",
				Type:            "events_api_v2_inbound_integration",
				Summary:         "cloudwatch alerts",
				MatchState:      ConfigMatchStateNotCompared,
				ManuallyCreated: true,
				DriftReason:     "manually_created",
			}},
			ObservedAt:   now,
			Redactions:   2,
			PagesFetched: 2,
		},
	}
	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "pagerduty-primary",
		Targets: []TargetConfig{{
			Provider:                ProviderPagerDuty,
			ScopeID:                 "pagerduty:account:example",
			AccountID:               "example",
			Token:                   "pd-token",
			IncidentLookback:        time.Hour,
			ConfigValidationEnabled: true,
			ConfigResourceLimit:     25,
		}},
		ClientFactory: func(TargetConfig) (EvidenceClient, error) { return client, nil },
		Now:           func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewClaimedSource() error = %v, want nil", err)
	}

	collected, ok, err := source.NextClaimed(context.Background(), testPagerDutyWorkItem(now))
	if err != nil {
		t.Fatalf("NextClaimed() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("NextClaimed() ok = false, want true")
	}
	envs := drainFacts(collected.Facts)
	counts := factKindCounts(envs)
	if got, want := counts[facts.IncidentRoutingObservedPagerDutyServiceFactKind], 1; got != want {
		t.Fatalf("observed service facts = %d, want %d; counts %#v", got, want, counts)
	}
	if got, want := counts[facts.IncidentRoutingObservedPagerDutyIntegrationFactKind], 1; got != want {
		t.Fatalf("observed integration facts = %d, want %d; counts %#v", got, want, counts)
	}
	for _, env := range envs {
		if strings.HasPrefix(env.FactKind, "work_item.") {
			t.Fatalf("FactKind = %q, PagerDuty collector must not emit work-item facts", env.FactKind)
		}
		if env.Payload["source_class"] == ConfigSourceClassObserved &&
			env.Payload["declared_match_state"] == "" {
			t.Fatalf("Payload = %#v, observed config fact must declare comparison state", env.Payload)
		}
	}
}

func TestClaimedSourceSkipsLiveConfigWhenDisabled(t *testing.T) {
	t.Parallel()

	now := testObservedAt()
	client := &recordingConfigEvidenceClient{
		staticConfigEvidenceClient: staticConfigEvidenceClient{
			result: CollectionResult{Incidents: []Incident{testIncident("P123")}, ObservedAt: now},
		},
	}
	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "pagerduty-primary",
		Targets: []TargetConfig{{
			Provider:         ProviderPagerDuty,
			ScopeID:          "pagerduty:account:example",
			AccountID:        "example",
			Token:            "pd-token",
			IncidentLookback: time.Hour,
		}},
		ClientFactory: func(TargetConfig) (EvidenceClient, error) { return client, nil },
		Now:           func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewClaimedSource() error = %v, want nil", err)
	}

	collected, ok, err := source.NextClaimed(context.Background(), testPagerDutyWorkItem(now))
	if err != nil || !ok {
		t.Fatalf("NextClaimed() = ok %v err %v, want ok true nil", ok, err)
	}
	if client.configCalls != 0 {
		t.Fatalf("config calls = %d, want 0 when config validation is disabled", client.configCalls)
	}
	counts := factKindCounts(drainFacts(collected.Facts))
	if got := counts[facts.IncidentRoutingObservedPagerDutyServiceFactKind]; got != 0 {
		t.Fatalf("observed service facts = %d, want 0", got)
	}
}

func TestClaimedSourceEmitsCoverageWarningForPartialLiveConfigFailures(t *testing.T) {
	t.Parallel()

	now := testObservedAt()
	client := staticConfigEvidenceClient{
		result: CollectionResult{ObservedAt: now},
		configResult: ConfigCollectionResult{
			Services: []ConfigService{{ID: "SVC1", Status: "active", MatchState: ConfigMatchStateNotCompared}},
			Warnings: []ConfigWarning{{
				ResourceClass: ConfigResourceClassService,
				ResourceID:    "SVC2",
				Reason:        ConfigWarningPermissionHidden,
			}},
			ObservedAt: now,
			Partial:    true,
		},
	}
	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "pagerduty-primary",
		Targets: []TargetConfig{{
			Provider:                ProviderPagerDuty,
			ScopeID:                 "pagerduty:account:example",
			AccountID:               "example",
			Token:                   "pd-token",
			IncidentLookback:        time.Hour,
			ConfigValidationEnabled: true,
			ConfigResourceLimit:     10,
		}},
		ClientFactory: func(TargetConfig) (EvidenceClient, error) { return client, nil },
		Now:           func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewClaimedSource() error = %v, want nil", err)
	}

	collected, ok, err := source.NextClaimed(context.Background(), testPagerDutyWorkItem(now))
	if err != nil || !ok {
		t.Fatalf("NextClaimed() = ok %v err %v, want ok true nil", ok, err)
	}
	envs := drainFacts(collected.Facts)
	counts := factKindCounts(envs)
	if got, want := counts[facts.IncidentRoutingCoverageWarningFactKind], 1; got != want {
		t.Fatalf("coverage warnings = %d, want %d; counts %#v", got, want, counts)
	}
	for _, env := range envs {
		if env.FactKind != facts.IncidentRoutingCoverageWarningFactKind {
			continue
		}
		if got, want := env.Payload["source_class"], ConfigSourceClassObserved; got != want {
			t.Fatalf("Payload[source_class] = %#v, want %#v", got, want)
		}
		if got, want := env.Payload["reason"], ConfigWarningPermissionHidden; got != want {
			t.Fatalf("Payload[reason] = %#v, want %#v", got, want)
		}
		return
	}
	t.Fatal("coverage warning fact not found")
}

func TestClaimedSourceReturnsRetryableFailureWhenLiveConfigRateLimited(t *testing.T) {
	t.Parallel()

	now := testObservedAt()
	client := staticConfigEvidenceClient{
		result:    CollectionResult{ObservedAt: now},
		configErr: PagerDutyError{StatusCode: 429, Message: "pd-token rate limited"},
	}
	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "pagerduty-primary",
		Targets: []TargetConfig{{
			Provider:                ProviderPagerDuty,
			ScopeID:                 "pagerduty:account:example",
			AccountID:               "example",
			Token:                   "pd-token",
			IncidentLookback:        time.Hour,
			ConfigValidationEnabled: true,
			ConfigResourceLimit:     10,
		}},
		ClientFactory: func(TargetConfig) (EvidenceClient, error) { return client, nil },
		Now:           func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewClaimedSource() error = %v, want nil", err)
	}

	_, _, err = source.NextClaimed(context.Background(), testPagerDutyWorkItem(now))
	if err == nil {
		t.Fatal("NextClaimed() error = nil, want rate-limit failure")
	}
	var classified interface{ FailureClass() string }
	if !errors.As(err, &classified) {
		t.Fatalf("NextClaimed() error = %T, want FailureClass", err)
	}
	if got, want := classified.FailureClass(), FailureRateLimited; got != want {
		t.Fatalf("FailureClass = %q, want %q", got, want)
	}
	if strings.Contains(err.Error(), "pd-token") {
		t.Fatalf("NextClaimed() error = %q, want token redacted", err)
	}
}

type staticConfigEvidenceClient struct {
	result       CollectionResult
	err          error
	configResult ConfigCollectionResult
	configErr    error
}

func (c staticConfigEvidenceClient) CollectIncidentEvidence(
	context.Context,
	TargetConfig,
	CollectionWindow,
) (CollectionResult, error) {
	return c.result, c.err
}

func (c staticConfigEvidenceClient) CollectConfigEvidence(
	context.Context,
	TargetConfig,
) (ConfigCollectionResult, error) {
	return c.configResult, c.configErr
}

type recordingConfigEvidenceClient struct {
	staticConfigEvidenceClient
	configCalls int
}

func (c *recordingConfigEvidenceClient) CollectConfigEvidence(
	ctx context.Context,
	target TargetConfig,
) (ConfigCollectionResult, error) {
	c.configCalls++
	return c.staticConfigEvidenceClient.CollectConfigEvidence(ctx, target)
}
