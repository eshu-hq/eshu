// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workflow

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
)

func TestDesiredCollectorInstanceValidateAcceptsDisabledHostedRegistrationWithBlankTargetFields(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name string
		kind scope.CollectorKind
	}{
		{name: "pagerduty", kind: scope.CollectorPagerDuty},
		{name: "jira", kind: scope.CollectorJira},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			instance := DesiredCollectorInstance{
				InstanceID:    tt.name + "-optional",
				CollectorKind: tt.kind,
				Mode:          CollectorModeContinuous,
				Enabled:       false,
				ClaimsEnabled: true,
				Configuration: `{"targets":[{}]}`,
			}

			if err := instance.Validate(); err != nil {
				t.Fatalf("Validate() error = %v, want nil", err)
			}
		})
	}
}

func TestCollectorInstanceValidateAcceptsDisabledHostedRegistrationWithBlankTargetFields(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.June, 3, 13, 30, 0, 0, time.UTC)
	for _, tt := range []struct {
		name string
		kind scope.CollectorKind
	}{
		{name: "pagerduty", kind: scope.CollectorPagerDuty},
		{name: "jira", kind: scope.CollectorJira},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			instance := CollectorInstance{
				InstanceID:     tt.name + "-optional",
				CollectorKind:  tt.kind,
				Mode:           CollectorModeContinuous,
				Enabled:        false,
				ClaimsEnabled:  true,
				Configuration:  `{"targets":[{}]}`,
				LastObservedAt: observedAt,
				CreatedAt:      observedAt,
				UpdatedAt:      observedAt,
			}

			if err := instance.Validate(); err != nil {
				t.Fatalf("Validate() error = %v, want nil", err)
			}
		})
	}
}
