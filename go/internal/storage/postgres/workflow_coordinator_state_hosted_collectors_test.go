// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestWorkflowControlStoreReconcileCollectorInstancesAcceptsDisabledHostedRegistrations(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	store := NewWorkflowControlStore(db)
	now := time.Date(2026, time.June, 3, 13, 30, 0, 0, time.UTC)

	err := store.ReconcileCollectorInstances(context.Background(), now, []workflow.DesiredCollectorInstance{
		{
			InstanceID:    "pagerduty-optional",
			CollectorKind: scope.CollectorPagerDuty,
			Mode:          workflow.CollectorModeContinuous,
			Enabled:       false,
			ClaimsEnabled: true,
			Configuration: `{"targets":[{}]}`,
		},
		{
			InstanceID:    "jira-optional",
			CollectorKind: scope.CollectorJira,
			Mode:          workflow.CollectorModeContinuous,
			Enabled:       false,
			ClaimsEnabled: true,
			Configuration: `{"targets":[{}]}`,
		},
	})
	if err != nil {
		t.Fatalf("ReconcileCollectorInstances() error = %v, want nil", err)
	}
	if got, want := len(db.execs), 3; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
}
