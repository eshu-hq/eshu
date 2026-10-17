// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package pagerduty

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
)

// TestClaimedSourceMapsAllowEmptySuccessorToTrustedDeletion proves the escape
// hatch wiring: only a target that explicitly attests its deletions carries
// the trusted-deletion signal the store backstop honors.
func TestClaimedSourceMapsAllowEmptySuccessorToTrustedDeletion(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.May, 31, 18, 30, 0, 0, time.UTC)
	collect := func(t *testing.T, allowEmpty bool) scope.ScopeGeneration {
		t.Helper()
		source, err := NewClaimedSource(SourceConfig{
			CollectorInstanceID: "pagerduty-primary",
			Targets: []TargetConfig{{
				Provider:            ProviderPagerDuty,
				ScopeID:             "pagerduty:account:example",
				AccountID:           "example",
				Token:               "pd-token",
				IncidentLookback:    6 * time.Hour,
				AllowEmptySuccessor: allowEmpty,
			}},
			ClientFactory: func(TargetConfig) (EvidenceClient, error) {
				return staticEvidenceClient{result: CollectionResult{ObservedAt: observedAt}}, nil
			},
			Now: func() time.Time { return observedAt },
		})
		if err != nil {
			t.Fatalf("NewClaimedSource() error = %v, want nil", err)
		}
		collected, ok, err := source.NextClaimed(context.Background(), testPagerDutyWorkItem(observedAt))
		if err != nil {
			t.Fatalf("NextClaimed() error = %v, want nil", err)
		}
		if !ok {
			t.Fatal("NextClaimed() ok = false, want true")
		}
		return collected.Generation
	}
	if got := collect(t, false); got.TrustedDeletion {
		t.Fatal("TrustedDeletion = true without AllowEmptySuccessor, want false")
	}
	if got := collect(t, true); !got.TrustedDeletion {
		t.Fatal("TrustedDeletion = false with AllowEmptySuccessor, want true")
	}
}
