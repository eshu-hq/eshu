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
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestClaimedSourceEmitsPagerDutyIncidentLifecycleAndChangeFacts(t *testing.T) {
	t.Parallel()

	now := testObservedAt()
	client := staticEvidenceClient{result: CollectionResult{
		Incidents: []Incident{
			testIncident("P123"),
			{
				ID:             "P124",
				IncidentNumber: 124,
				Title:          "resolved checkout-api error budget",
				Status:         "resolved",
				Service:        Reference{ID: "SVC1", Summary: "checkout-api"},
				CreatedAt:      now.Add(-2 * time.Hour),
				UpdatedAt:      now.Add(-90 * time.Minute),
				ResolvedAt:     now.Add(-80 * time.Minute),
				HTMLURL:        "https://example.pagerduty.com/incidents/P124",
			},
		},
		LifecycleEvents: map[string][]LifecycleEvent{
			"P123": {{
				ID:         "R1",
				IncidentID: "P123",
				Type:       "acknowledge_log_entry",
				CreatedAt:  now.Add(-4 * time.Minute),
			}},
		},
		RelatedChangeEvents: map[string][]ChangeEvent{
			"P123": {{
				ID:        "CE1",
				Summary:   "Deploy checkout-api",
				Source:    "github",
				Timestamp: now.Add(-20 * time.Minute),
				HTMLURL:   "https://example.pagerduty.com/change_events/CE1",
			}},
		},
		ObservedAt: now,
	}}
	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "pagerduty-primary",
		Targets: []TargetConfig{{
			Provider:          ProviderPagerDuty,
			ScopeID:           "pagerduty:account:example",
			AccountID:         "example",
			Token:             "pd-token",
			APIBaseURL:        "https://api.pagerduty.com",
			SourceURI:         "https://example.pagerduty.com?token=secret",
			IncidentLimit:     50,
			IncidentLookback:  6 * time.Hour,
			LogEntryLimit:     100,
			ChangeEventLimit:  50,
			AllowedServiceIDs: []string{"SVC1"},
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
	if got, want := collected.Scope.ScopeKind, scope.KindPagerDutyAccount; got != want {
		t.Fatalf("ScopeKind = %q, want %q", got, want)
	}
	envs := drainFacts(collected.Facts)
	counts := factKindCounts(envs)
	wantCounts := map[string]int{
		facts.IncidentRecordFactKind:         2,
		facts.IncidentLifecycleEventFactKind: 1,
		facts.ChangeRecordFactKind:           1,
	}
	for kind, want := range wantCounts {
		if got := counts[kind]; got != want {
			t.Fatalf("fact count %s = %d, want %d; all counts %#v", kind, got, want, counts)
		}
	}
	for _, env := range envs {
		if strings.HasPrefix(env.FactKind, "work_item.") {
			t.Fatalf("FactKind = %q, PagerDuty collector must not emit Jira/work-item facts", env.FactKind)
		}
		if env.SourceConfidence != facts.SourceConfidenceReported {
			t.Fatalf("SourceConfidence = %q, want reported", env.SourceConfidence)
		}
	}
}

func TestClaimedSourceEmitsCoverageWarningForDeniedRelatedChangeEvents(t *testing.T) {
	t.Parallel()

	now := testObservedAt()
	client := staticEvidenceClient{result: CollectionResult{
		Incidents: []Incident{testIncident("P123")},
		LifecycleEvents: map[string][]LifecycleEvent{
			"P123": {{
				ID:         "R1",
				IncidentID: "P123",
				Type:       "acknowledge_log_entry",
				CreatedAt:  now.Add(-4 * time.Minute),
			}},
		},
		Warnings: []ConfigWarning{{
			ResourceClass: ConfigResourceClassRelatedChangeEvent,
			ResourceID:    "P123",
			Reason:        ConfigWarningPermissionHidden,
		}},
		ObservedAt: now,
	}}
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
	envs := drainFacts(collected.Facts)
	counts := factKindCounts(envs)
	if got, want := counts[facts.IncidentRecordFactKind], 1; got != want {
		t.Fatalf("incident facts = %d, want %d; counts %#v", got, want, counts)
	}
	if got, want := counts[facts.IncidentLifecycleEventFactKind], 1; got != want {
		t.Fatalf("lifecycle facts = %d, want %d; counts %#v", got, want, counts)
	}
	if got, want := counts[facts.IncidentRoutingCoverageWarningFactKind], 1; got != want {
		t.Fatalf("coverage warning facts = %d, want %d; counts %#v", got, want, counts)
	}
	for _, env := range envs {
		if env.FactKind != facts.IncidentRoutingCoverageWarningFactKind {
			continue
		}
		if got, want := env.Payload["resource_class"], ConfigResourceClassRelatedChangeEvent; got != want {
			t.Fatalf("warning resource_class = %#v, want %#v", got, want)
		}
		if got, want := env.Payload["reason"], ConfigWarningPermissionHidden; got != want {
			t.Fatalf("warning reason = %#v, want %#v", got, want)
		}
		return
	}
	t.Fatal("coverage warning fact not found")
}

func TestClaimedSourceUsesProviderNativeStableKeysAcrossDuplicateWindows(t *testing.T) {
	t.Parallel()

	now := testObservedAt()
	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "pagerduty-primary",
		Targets: []TargetConfig{{
			Provider:         ProviderPagerDuty,
			ScopeID:          "pagerduty:account:example",
			AccountID:        "example",
			Token:            "pd-token",
			IncidentLookback: time.Hour,
		}},
		ClientFactory: func(TargetConfig) (EvidenceClient, error) {
			return staticEvidenceClient{result: CollectionResult{
				Incidents:  []Incident{testIncident("P123")},
				ObservedAt: now,
			}}, nil
		},
		Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewClaimedSource() error = %v, want nil", err)
	}

	first, ok, err := source.NextClaimed(context.Background(), testPagerDutyWorkItem(now))
	if err != nil || !ok {
		t.Fatalf("first NextClaimed() = ok %v err %v, want ok true nil", ok, err)
	}
	secondItem := testPagerDutyWorkItem(now.Add(time.Minute))
	secondItem.GenerationID = "pagerduty:generation-2"
	secondItem.WorkItemID = "pagerduty:pagerduty-primary:generation-2"
	second, ok, err := source.NextClaimed(context.Background(), secondItem)
	if err != nil || !ok {
		t.Fatalf("second NextClaimed() = ok %v err %v, want ok true nil", ok, err)
	}

	firstEnv := drainFacts(first.Facts)[0]
	secondEnv := drainFacts(second.Facts)[0]
	if firstEnv.StableFactKey != secondEnv.StableFactKey {
		t.Fatalf("StableFactKey changed across duplicate windows: %q vs %q", firstEnv.StableFactKey, secondEnv.StableFactKey)
	}
	if first.Generation.FreshnessHint != second.Generation.FreshnessHint {
		t.Fatalf("FreshnessHint changed for unchanged incidents: %q vs %q", first.Generation.FreshnessHint, second.Generation.FreshnessHint)
	}
}

func TestClaimedSourceClassifiesAuthDeniedAndRateLimitedFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		err       error
		wantClass string
	}{
		{
			name:      "auth denied",
			err:       PagerDutyError{StatusCode: 401, Message: "pd-token cannot read P123"},
			wantClass: FailureAuthDenied,
		},
		{
			name:      "rate limited",
			err:       PagerDutyError{StatusCode: 429, Message: "pd-token hit rate limit for P123"},
			wantClass: FailureRateLimited,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			now := testObservedAt()
			source, err := NewClaimedSource(SourceConfig{
				CollectorInstanceID: "pagerduty-primary",
				Targets: []TargetConfig{{
					Provider:         ProviderPagerDuty,
					ScopeID:          "pagerduty:account:example",
					AccountID:        "example",
					Token:            "pd-token",
					IncidentLookback: time.Hour,
				}},
				ClientFactory: func(TargetConfig) (EvidenceClient, error) {
					return staticEvidenceClient{err: tt.err}, nil
				},
				Now: func() time.Time { return now },
			})
			if err != nil {
				t.Fatalf("NewClaimedSource() error = %v, want nil", err)
			}

			_, _, err = source.NextClaimed(context.Background(), testPagerDutyWorkItem(now))
			if err == nil {
				t.Fatal("NextClaimed() error = nil, want classified provider failure")
			}
			if strings.Contains(err.Error(), "pd-token") || strings.Contains(err.Error(), "P123") {
				t.Fatalf("NextClaimed() error = %q, want bounded redacted message", err)
			}
			var classified interface{ FailureClass() string }
			if !errors.As(err, &classified) {
				t.Fatalf("NextClaimed() error = %T, want FailureClass", err)
			}
			if got := classified.FailureClass(); got != tt.wantClass {
				t.Fatalf("FailureClass = %q, want %q", got, tt.wantClass)
			}
		})
	}
}

type staticEvidenceClient struct {
	result CollectionResult
	err    error
}

func (c staticEvidenceClient) CollectIncidentEvidence(context.Context, TargetConfig, CollectionWindow) (CollectionResult, error) {
	return c.result, c.err
}

func testPagerDutyWorkItem(now time.Time) workflow.WorkItem {
	return workflow.WorkItem{
		WorkItemID:          "pagerduty:pagerduty-primary:generation-1",
		RunID:               "pagerduty:pagerduty-primary:schedule-1",
		CollectorKind:       scope.CollectorPagerDuty,
		CollectorInstanceID: "pagerduty-primary",
		SourceSystem:        string(scope.CollectorPagerDuty),
		ScopeID:             "pagerduty:account:example",
		AcceptanceUnitID:    "pagerduty:account:example",
		SourceRunID:         "pagerduty:generation-1",
		GenerationID:        "pagerduty:generation-1",
		FairnessKey:         "pagerduty:example",
		Status:              workflow.WorkItemStatusPending,
		CurrentFencingToken: 42,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
}

func drainFacts(in <-chan facts.Envelope) []facts.Envelope {
	var envs []facts.Envelope
	for env := range in {
		envs = append(envs, env)
	}
	return envs
}

func factKindCounts(envs []facts.Envelope) map[string]int {
	counts := make(map[string]int, len(envs))
	for _, env := range envs {
		counts[env.FactKind]++
	}
	return counts
}
