// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package grafanaplanner

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
	tests := []struct {
		name    string
		mutate  func(*PlanRequest)
		wantErr string
	}{
		{
			name: "wrong collector kind",
			mutate: func(request *PlanRequest) {
				request.Instance.CollectorKind = scope.CollectorGit
			},
			wantErr: `grafana planner requires collector_kind "grafana"`,
		},
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
			request := validEdgeRequest(observedAt, `{"targets":[]}`)
			test.mutate(&request)
			_, _, err := (WorkPlanner{}).PlanGrafanaWork(t.Context(), request)
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("PlanGrafanaWork() error = %v, want containing %q", err, test.wantErr)
			}
		})
	}
}

func TestWorkPlannerValidatesAllTargetsBeforeDisabledAndScopeFilters(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.June, 5, 18, 30, 0, 0, time.UTC)
	tests := []struct {
		name          string
		configuration string
		wantErr       string
	}{
		{
			name: "blank disabled scope",
			configuration: `{"targets":[
				{"provider":"grafana","scope_id":" ","instance_id":"blank","enabled":false}
			]}`,
			wantErr: "grafana target scope_id must not be blank",
		},
		{
			name: "duplicate disabled scope",
			configuration: `{"targets":[
				{"provider":"grafana","scope_id":"grafana:instance:duplicate","instance_id":"first","enabled":false},
				{"provider":"grafana","scope_id":" grafana:instance:duplicate ","instance_id":"second","enabled":false}
			]}`,
			wantErr: "duplicate grafana target scope_id",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			request := validEdgeRequest(observedAt, test.configuration)
			request.ScopeIDs = []string{"grafana:instance:not-present"}
			_, _, err := (WorkPlanner{}).PlanGrafanaWork(t.Context(), request)
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("PlanGrafanaWork() error = %v, want containing %q", err, test.wantErr)
			}
		})
	}
}

func TestWorkPlannerReturnsPopulatedRunForEmptySelection(t *testing.T) {
	t.Parallel()

	location := time.FixedZone("EDT", -4*60*60)
	observedAt := time.Date(2026, time.June, 5, 18, 30, 0, 0, location)
	tests := []struct {
		name          string
		configuration string
		scopeIDs      []string
	}{
		{
			name: "all targets disabled",
			configuration: `{"targets":[
				{"provider":"grafana","scope_id":"grafana:instance:disabled","instance_id":"disabled","enabled":false}
			]}`,
		},
		{
			name: "nonmatching scope filter",
			configuration: `{"targets":[
				{"provider":"grafana","scope_id":"grafana:instance:enabled","instance_id":"enabled","enabled":true}
			]}`,
			scopeIDs: []string{"grafana:instance:not-present"},
		},
		{
			name:          "empty configuration",
			configuration: "",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			request := validEdgeRequest(observedAt, test.configuration)
			request.ScopeIDs = test.scopeIDs
			run, items, err := (WorkPlanner{}).PlanGrafanaWork(t.Context(), request)
			if err != nil {
				t.Fatalf("PlanGrafanaWork() error = %v, want nil", err)
			}
			if got := len(items); got != 0 {
				t.Fatalf("len(items) = %d, want 0", got)
			}
			if got, want := run.RunID, "grafana:grafana-primary:schedule:schedule-20260605T180000Z"; got != want {
				t.Fatalf("RunID = %q, want %q", got, want)
			}
			if got, want := run.TriggerKind, workflow.TriggerKindSchedule; got != want {
				t.Fatalf("TriggerKind = %q, want %q", got, want)
			}
			if got, want := run.Status, workflow.RunStatusCollectionPending; got != want {
				t.Fatalf("Status = %q, want %q", got, want)
			}
			if got, want := run.RequestedCollector, string(scope.CollectorGrafana); got != want {
				t.Fatalf("RequestedCollector = %q, want %q", got, want)
			}
			if got, want := run.CreatedAt, observedAt.UTC(); !got.Equal(want) || got.Location() != time.UTC {
				t.Fatalf("CreatedAt = %v (%v), want %v (UTC)", got, got.Location(), want)
			}
			if got, want := run.UpdatedAt, observedAt.UTC(); !got.Equal(want) || got.Location() != time.UTC {
				t.Fatalf("UpdatedAt = %v (%v), want %v (UTC)", got, got.Location(), want)
			}
			if got, want := run.RequestedScopeSet, `{"collector_instance_id":"grafana-primary","targets":[]}`; got != want {
				t.Fatalf("RequestedScopeSet = %q, want %q", got, want)
			}
		})
	}
}

func TestWorkPlannerDerivesTriggerKindWhenRequestOmitsIt(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.June, 5, 18, 30, 0, 0, time.UTC)
	planKey := "same-plan-key"
	tests := []struct {
		name        string
		bootstrap   bool
		wantTrigger workflow.TriggerKind
		wantRunID   string
	}{
		{
			name:        "schedule",
			wantTrigger: workflow.TriggerKindSchedule,
			wantRunID:   "grafana:grafana-primary:schedule:same-plan-key",
		},
		{
			name:        "bootstrap",
			bootstrap:   true,
			wantTrigger: workflow.TriggerKindBootstrap,
			wantRunID:   "grafana:grafana-primary:bootstrap:same-plan-key",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			request := validEdgeRequest(observedAt, `{"targets":[]}`)
			request.Instance.Bootstrap = test.bootstrap
			request.PlanKey = planKey
			request.TriggerKind = ""

			run, _, err := (WorkPlanner{}).PlanGrafanaWork(t.Context(), request)
			if err != nil {
				t.Fatalf("PlanGrafanaWork() error = %v, want nil", err)
			}
			if got := run.TriggerKind; got != test.wantTrigger {
				t.Fatalf("TriggerKind = %q, want %q", got, test.wantTrigger)
			}
			if got := run.RunID; got != test.wantRunID {
				t.Fatalf("RunID = %q, want %q", got, test.wantRunID)
			}
		})
	}
}

func validEdgeRequest(observedAt time.Time, configuration string) PlanRequest {
	return PlanRequest{
		Instance: workflow.CollectorInstance{
			InstanceID:     "grafana-primary",
			CollectorKind:  scope.CollectorGrafana,
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
