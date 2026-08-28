// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package prometheusmimir

import (
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestWorkPlannerRejectsInvalidRequestAndConfiguration(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.June, 5, 18, 30, 0, 0, time.UTC)
	validInstance := workflow.CollectorInstance{
		InstanceID:     "prometheus-mimir-primary",
		CollectorKind:  scope.CollectorPrometheusMimir,
		Mode:           workflow.CollectorModeContinuous,
		Enabled:        true,
		ClaimsEnabled:  true,
		Configuration:  `{"targets":[]}`,
		LastObservedAt: observedAt,
		CreatedAt:      observedAt,
		UpdatedAt:      observedAt,
	}
	validRequest := PlanRequest{
		Instance:   validInstance,
		ObservedAt: observedAt,
		PlanKey:    "schedule-20260605T180000Z",
	}

	tests := []struct {
		name    string
		mutate  func(*PlanRequest)
		wantErr string
	}{
		{
			name: "disabled instance",
			mutate: func(request *PlanRequest) {
				request.Instance.Enabled = false
			},
			wantErr: "requires enabled collector instance",
		},
		{
			name: "claims disabled",
			mutate: func(request *PlanRequest) {
				request.Instance.ClaimsEnabled = false
			},
			wantErr: "requires claim-enabled collector instance",
		},
		{
			name: "zero observed time",
			mutate: func(request *PlanRequest) {
				request.ObservedAt = time.Time{}
			},
			wantErr: "observed_at must not be zero",
		},
		{
			name: "unsafe plan key",
			mutate: func(request *PlanRequest) {
				request.PlanKey = "raw/source"
			},
			wantErr: "must not include raw source locator material",
		},
		{
			name: "invalid explicit trigger",
			mutate: func(request *PlanRequest) {
				request.TriggerKind = workflow.TriggerKind("invalid")
			},
			wantErr: "unknown workflow trigger kind",
		},
		{
			name: "malformed configuration",
			mutate: func(request *PlanRequest) {
				request.Instance.Configuration = `{"targets":`
			},
			wantErr: "configuration must be valid JSON",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			request := validRequest
			test.mutate(&request)
			_, _, err := (WorkPlanner{}).PlanPrometheusMimirWork(t.Context(), request)
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("PlanPrometheusMimirWork() error = %v, want containing %q", err, test.wantErr)
			}
		})
	}
}

func TestWorkPlannerValidatesEnabledTargetsBeforeScopeFilter(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.June, 5, 18, 30, 0, 0, time.UTC)
	tests := []struct {
		name          string
		configuration string
		wantErr       string
	}{
		{
			name: "blank enabled scope",
			configuration: `{"targets":[
				{"provider":"prometheus","scope_id":" ","instance_id":"blank","enabled":true}
			]}`,
			wantErr: "target scope_id must not be blank",
		},
		{
			name: "duplicate enabled scope",
			configuration: `{"targets":[
				{"provider":"prometheus","scope_id":"prometheus:source:duplicate","instance_id":"first","enabled":true},
				{"provider":"mimir","scope_id":"prometheus:source:duplicate","instance_id":"second","enabled":true}
			]}`,
			wantErr: "duplicate prometheus/mimir target scope_id",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			request := validEdgeRequest(observedAt, test.configuration)
			request.ScopeIDs = []string{"prometheus:source:not-present"}
			_, _, err := (WorkPlanner{}).PlanPrometheusMimirWork(t.Context(), request)
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("PlanPrometheusMimirWork() error = %v, want containing %q", err, test.wantErr)
			}
		})
	}
}

func TestWorkPlannerReturnsPopulatedRunForEmptySelection(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.June, 5, 18, 30, 0, 0, time.UTC)
	tests := []struct {
		name          string
		configuration string
		scopeIDs      []string
	}{
		{
			name: "nonmatching scope filter",
			configuration: `{"targets":[
				{"provider":"prometheus","scope_id":"prometheus:source:enabled","instance_id":"enabled","enabled":true}
			]}`,
			scopeIDs: []string{"prometheus:source:not-present"},
		},
		{
			name: "disabled invalid targets are dropped",
			configuration: `{"targets":[
				{"provider":"prometheus","scope_id":" ","instance_id":"blank","enabled":false},
				{"provider":"mimir","scope_id":"duplicate","instance_id":"first","enabled":false},
				{"provider":"mimir","scope_id":"duplicate","instance_id":"second","enabled":false}
			]}`,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			request := validEdgeRequest(observedAt, test.configuration)
			request.ScopeIDs = test.scopeIDs
			run, items, err := (WorkPlanner{}).PlanPrometheusMimirWork(t.Context(), request)
			if err != nil {
				t.Fatalf("PlanPrometheusMimirWork() error = %v, want nil", err)
			}
			if len(items) != 0 {
				t.Fatalf("len(items) = %d, want 0", len(items))
			}
			if got, want := run.RunID, "prometheus_mimir:prometheus-mimir-primary:schedule:schedule-20260605T180000Z"; got != want {
				t.Fatalf("RunID = %q, want %q", got, want)
			}
			if got, want := run.TriggerKind, workflow.TriggerKindSchedule; got != want {
				t.Fatalf("TriggerKind = %q, want %q", got, want)
			}
			if got, want := run.Status, workflow.RunStatusCollectionPending; got != want {
				t.Fatalf("Status = %q, want %q", got, want)
			}
			if got, want := run.RequestedCollector, string(scope.CollectorPrometheusMimir); got != want {
				t.Fatalf("RequestedCollector = %q, want %q", got, want)
			}
			if got, want := run.CreatedAt, observedAt.UTC(); !got.Equal(want) || got.Location() != time.UTC {
				t.Fatalf("CreatedAt = %v (%v), want %v (UTC)", got, got.Location(), want)
			}
			if got, want := run.UpdatedAt, observedAt.UTC(); !got.Equal(want) || got.Location() != time.UTC {
				t.Fatalf("UpdatedAt = %v (%v), want %v (UTC)", got, got.Location(), want)
			}
			if got, want := run.RequestedScopeSet, `{"collector_instance_id":"prometheus-mimir-primary","targets":[]}`; got != want {
				t.Fatalf("RequestedScopeSet = %q, want %q", got, want)
			}
		})
	}
}

func validEdgeRequest(observedAt time.Time, configuration string) PlanRequest {
	return PlanRequest{
		Instance: workflow.CollectorInstance{
			InstanceID:     "prometheus-mimir-primary",
			CollectorKind:  scope.CollectorPrometheusMimir,
			Mode:           workflow.CollectorModeContinuous,
			Enabled:        true,
			ClaimsEnabled:  true,
			Configuration:  configuration,
			LastObservedAt: observedAt,
			CreatedAt:      observedAt,
			UpdatedAt:      observedAt,
		},
		ObservedAt: observedAt,
		PlanKey:    "schedule-20260605T180000Z",
	}
}
