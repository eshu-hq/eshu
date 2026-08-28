// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package tempoplanner

import (
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestWorkPlannerValidatesRequestContract(t *testing.T) {
	t.Parallel()

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
			wantErr: "tempo planner requires enabled collector instance",
		},
		{
			name: "claims disabled",
			mutate: func(request *PlanRequest) {
				request.Instance.ClaimsEnabled = false
			},
			wantErr: "tempo planner requires claim-enabled collector instance",
		},
		{
			name: "zero observed at",
			mutate: func(request *PlanRequest) {
				request.ObservedAt = time.Time{}
			},
			wantErr: "tempo planner observed_at must not be zero",
		},
		{
			name: "blank plan key",
			mutate: func(request *PlanRequest) {
				request.PlanKey = "   "
			},
			wantErr: "tempo planner plan_key must not be blank",
		},
		{
			name: "path plan key",
			mutate: func(request *PlanRequest) {
				request.PlanKey = "schedule/source"
			},
			wantErr: "tempo planner plan_key must not include raw source locator material",
		},
		{
			name: "backslash plan key",
			mutate: func(request *PlanRequest) {
				request.PlanKey = `schedule\source`
			},
			wantErr: "tempo planner plan_key must not include raw source locator material",
		},
		{
			name: "delimiter plan key",
			mutate: func(request *PlanRequest) {
				request.PlanKey = "schedule:source"
			},
			wantErr: "tempo planner plan_key contains unsupported character ':'",
		},
		{
			name: "unicode plan key",
			mutate: func(request *PlanRequest) {
				request.PlanKey = "schedule-é"
			},
			wantErr: "tempo planner plan_key contains unsupported character 'é'",
		},
		{
			name: "invalid trigger kind",
			mutate: func(request *PlanRequest) {
				request.TriggerKind = workflow.TriggerKind("timer")
			},
			wantErr: `unknown workflow trigger kind "timer"`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			request := validTempoPlanRequest(`{"targets":[]}`)
			test.mutate(&request)
			_, _, err := (WorkPlanner{}).PlanTempoWork(t.Context(), request)
			if err == nil || err.Error() != test.wantErr {
				t.Fatalf("PlanTempoWork() error = %v, want %q", err, test.wantErr)
			}
		})
	}
}

func TestWorkPlannerAcceptsEveryExplicitTriggerKind(t *testing.T) {
	t.Parallel()

	triggerKinds := []workflow.TriggerKind{
		workflow.TriggerKindBootstrap,
		workflow.TriggerKindSchedule,
		workflow.TriggerKindWebhook,
		workflow.TriggerKindReplay,
		workflow.TriggerKindOperatorRecovery,
	}
	for _, triggerKind := range triggerKinds {
		triggerKind := triggerKind
		t.Run(string(triggerKind), func(t *testing.T) {
			t.Parallel()
			request := validTempoPlanRequest(`{"targets":[]}`)
			request.Instance.Bootstrap = triggerKind != workflow.TriggerKindBootstrap
			request.TriggerKind = triggerKind
			run, _, err := (WorkPlanner{}).PlanTempoWork(t.Context(), request)
			if err != nil {
				t.Fatalf("PlanTempoWork() error = %v, want nil", err)
			}
			if got := run.TriggerKind; got != triggerKind {
				t.Fatalf("TriggerKind = %q, want explicit %q", got, triggerKind)
			}
		})
	}
}

func TestWorkPlannerValidatesTargetsBeforeFiltering(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		configuration string
		wantErr       string
	}{
		{
			name:          "malformed json",
			configuration: `{"targets":[`,
			wantErr:       "tempo plan request: configuration must be valid JSON",
		},
		{
			name:          "missing scope",
			configuration: `{"targets":[{"scope_id":"   ","enabled":true}]}`,
			wantErr:       "tempo target[0] requires scope_id",
		},
		{
			name: "duplicate after trimming includes disabled target",
			configuration: `{"targets":[
				{"scope_id":"tempo:source:a","enabled":true},
				{"scope_id":" tempo:source:a ","enabled":false}
			]}`,
			wantErr: `duplicate tempo target scope_id "tempo:source:a"`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			request := validTempoPlanRequest(test.configuration)
			request.ScopeIDs = []string{"tempo:source:unknown"}
			_, _, err := (WorkPlanner{}).PlanTempoWork(t.Context(), request)
			if err == nil || err.Error() != test.wantErr {
				t.Fatalf("PlanTempoWork() error = %v, want %q", err, test.wantErr)
			}
		})
	}
}

func TestWorkPlannerScopeFilteringPreservesConfiguredItemOrder(t *testing.T) {
	t.Parallel()

	configuration := `{"targets":[
		{"scope_id":" tempo:source:z ","instance_id":" zeta ","base_url":" https://z.example.com ","enabled":true},
		{"scope_id":"tempo:source:disabled","enabled":false},
		{"scope_id":"tempo:source:a","instance_id":" alpha ","base_url":"","enabled":true}
	]}`
	tests := []struct {
		name     string
		scopeIDs []string
		want     []string
	}{
		{name: "nil selects all enabled", want: []string{"tempo:source:z", "tempo:source:a"}},
		{name: "subset ignores request order", scopeIDs: []string{"tempo:source:a", "tempo:source:z"}, want: []string{"tempo:source:z", "tempo:source:a"}},
		{name: "unknown selects none", scopeIDs: []string{"tempo:source:unknown"}},
		{name: "blanks only select none", scopeIDs: []string{" ", "\t"}},
		{name: "duplicates select once", scopeIDs: []string{"tempo:source:a", " tempo:source:a "}, want: []string{"tempo:source:a"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			request := validTempoPlanRequest(configuration)
			request.ScopeIDs = test.scopeIDs
			run, items, err := (WorkPlanner{}).PlanTempoWork(t.Context(), request)
			if err != nil {
				t.Fatalf("PlanTempoWork() error = %v, want nil", err)
			}
			if got, want := len(items), len(test.want); got != want {
				t.Fatalf("len(items) = %d, want %d", got, want)
			}
			for i, want := range test.want {
				if got := items[i].ScopeID; got != want {
					t.Fatalf("items[%d].ScopeID = %q, want %q", i, got, want)
				}
			}
			if len(items) == 2 {
				wantScopeSet := `{"collector_instance_id":"tempo-primary","targets":[{"scope_id":"tempo:source:a","instance_id":"alpha","base_url":""},{"scope_id":"tempo:source:z","instance_id":"zeta","base_url":"https://z.example.com"}]}`
				if got := run.RequestedScopeSet; got != wantScopeSet {
					t.Fatalf("RequestedScopeSet = %q, want sorted %q", got, wantScopeSet)
				}
			}
		})
	}
}

func TestWorkPlannerBlankConfigurationAndTriggerPrecedence(t *testing.T) {
	t.Parallel()

	for _, configuration := range []string{"", "   ", "{}", `{"targets":null}`} {
		request := validTempoPlanRequest(configuration)
		request.Instance.Bootstrap = true
		request.TriggerKind = workflow.TriggerKindOperatorRecovery
		run, items, err := (WorkPlanner{}).PlanTempoWork(t.Context(), request)
		if err != nil {
			t.Fatalf("PlanTempoWork(%q) error = %v, want nil", configuration, err)
		}
		if got, want := run.TriggerKind, workflow.TriggerKindOperatorRecovery; got != want {
			t.Fatalf("TriggerKind = %q, want explicit %q", got, want)
		}
		if got := len(items); got != 0 {
			t.Fatalf("len(items) = %d, want 0", got)
		}
	}

	request := validTempoPlanRequest(`{"targets":[]}`)
	request.Instance.Bootstrap = true
	run, _, err := (WorkPlanner{}).PlanTempoWork(t.Context(), request)
	if err != nil {
		t.Fatalf("PlanTempoWork() error = %v, want nil", err)
	}
	if got, want := run.TriggerKind, workflow.TriggerKindBootstrap; got != want {
		t.Fatalf("TriggerKind = %q, want derived %q", got, want)
	}
	if !strings.Contains(run.RunID, ":bootstrap:") {
		t.Fatalf("RunID = %q, want bootstrap trigger component", run.RunID)
	}
}

func validTempoPlanRequest(configuration string) PlanRequest {
	observedAt := time.Date(2026, time.June, 5, 18, 30, 0, 0, time.UTC)
	return PlanRequest{
		Instance: workflow.CollectorInstance{
			InstanceID:     "tempo-primary",
			CollectorKind:  scope.CollectorTempo,
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
