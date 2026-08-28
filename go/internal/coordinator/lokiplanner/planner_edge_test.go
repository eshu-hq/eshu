// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package lokiplanner

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
			name: "wrong collector kind",
			mutate: func(request *PlanRequest) {
				request.Instance.CollectorKind = scope.CollectorGit
			},
			wantErr: `loki planner requires collector_kind "loki"`,
		},
		{
			name: "disabled instance",
			mutate: func(request *PlanRequest) {
				request.Instance.Enabled = false
			},
			wantErr: "loki planner requires enabled collector instance",
		},
		{
			name: "claims disabled",
			mutate: func(request *PlanRequest) {
				request.Instance.ClaimsEnabled = false
			},
			wantErr: "loki planner requires claim-enabled collector instance",
		},
		{
			name: "zero observed at",
			mutate: func(request *PlanRequest) {
				request.ObservedAt = time.Time{}
			},
			wantErr: "loki planner observed_at must not be zero",
		},
		{
			name: "blank plan key",
			mutate: func(request *PlanRequest) {
				request.PlanKey = "   "
			},
			wantErr: "loki planner plan_key must not be blank",
		},
		{
			name: "path plan key",
			mutate: func(request *PlanRequest) {
				request.PlanKey = "schedule/source"
			},
			wantErr: "loki planner plan_key must not include raw source locator material",
		},
		{
			name: "backslash plan key",
			mutate: func(request *PlanRequest) {
				request.PlanKey = `schedule\source`
			},
			wantErr: "loki planner plan_key must not include raw source locator material",
		},
		{
			name: "delimiter plan key",
			mutate: func(request *PlanRequest) {
				request.PlanKey = "schedule:source"
			},
			wantErr: "loki planner plan_key contains unsupported character ':'",
		},
		{
			name: "unicode plan key",
			mutate: func(request *PlanRequest) {
				request.PlanKey = "schedule-é"
			},
			wantErr: "loki planner plan_key contains unsupported character 'é'",
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
			request := validLokiPlanRequest(`{"targets":[]}`)
			test.mutate(&request)
			_, _, err := (WorkPlanner{}).PlanLokiWork(t.Context(), request)
			if err == nil || err.Error() != test.wantErr {
				t.Fatalf("PlanLokiWork() error = %v, want %q", err, test.wantErr)
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
			request := validLokiPlanRequest(`{"targets":[]}`)
			request.Instance.Bootstrap = triggerKind != workflow.TriggerKindBootstrap
			request.TriggerKind = triggerKind
			run, _, err := (WorkPlanner{}).PlanLokiWork(t.Context(), request)
			if err != nil {
				t.Fatalf("PlanLokiWork() error = %v, want nil", err)
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
			wantErr:       "loki plan request: configuration must be valid JSON",
		},
		{
			name:          "missing scope",
			configuration: `{"targets":[{"enabled":true}]}`,
			wantErr:       "loki target scope_id must not be blank",
		},
		{
			name:          "blank scope",
			configuration: `{"targets":[{"scope_id":"   ","enabled":false}]}`,
			wantErr:       "loki target scope_id must not be blank",
		},
		{
			name: "duplicate after trimming includes disabled target",
			configuration: `{"targets":[
				{"scope_id":"loki:source:a","enabled":true},
				{"scope_id":" loki:source:a ","enabled":false}
			]}`,
			wantErr: `duplicate loki target scope_id "loki:source:a"`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			request := validLokiPlanRequest(test.configuration)
			request.ScopeIDs = []string{"loki:source:unknown"}
			_, _, err := (WorkPlanner{}).PlanLokiWork(t.Context(), request)
			if err == nil || err.Error() != test.wantErr {
				t.Fatalf("PlanLokiWork() error = %v, want %q", err, test.wantErr)
			}
		})
	}
}

func TestWorkPlannerScopeFilteringPreservesConfiguredItemOrder(t *testing.T) {
	t.Parallel()

	configuration := `{"targets":[
		{"scope_id":" loki:source:z ","instance_id":" zeta ","base_url":" https://z.example.com ","enabled":true},
		{"scope_id":"loki:source:disabled","enabled":false},
		{"scope_id":"loki:source:a","instance_id":" alpha ","base_url":"","enabled":true}
	]}`
	tests := []struct {
		name     string
		scopeIDs []string
		want     []string
	}{
		{name: "nil selects all enabled", want: []string{"loki:source:z", "loki:source:a"}},
		{name: "subset ignores request order", scopeIDs: []string{"loki:source:a", "loki:source:z"}, want: []string{"loki:source:z", "loki:source:a"}},
		{name: "unknown selects none", scopeIDs: []string{"loki:source:unknown"}},
		{name: "blanks only select none", scopeIDs: []string{" ", "\t"}},
		{name: "duplicates select once", scopeIDs: []string{"loki:source:a", " loki:source:a "}, want: []string{"loki:source:a"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			request := validLokiPlanRequest(configuration)
			request.ScopeIDs = test.scopeIDs
			run, items, err := (WorkPlanner{}).PlanLokiWork(t.Context(), request)
			if err != nil {
				t.Fatalf("PlanLokiWork() error = %v, want nil", err)
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
				wantScopeSet := `{"collector_instance_id":"loki-primary","targets":[{"scope_id":"loki:source:a","instance_id":"alpha","base_url":""},{"scope_id":"loki:source:z","instance_id":"zeta","base_url":"https://z.example.com"}]}`
				if got := run.RequestedScopeSet; got != wantScopeSet {
					t.Fatalf("RequestedScopeSet = %q, want sorted %q", got, wantScopeSet)
				}
			}
		})
	}
}

func TestWorkPlannerBlankConfigurationAndTriggerPrecedence(t *testing.T) {
	t.Parallel()

	for _, configuration := range []string{
		"",
		"   ",
		"{}",
		`{"targets":null}`,
		`{"targets":[{"scope_id":"loki:source:disabled","enabled":false}]}`,
	} {
		request := validLokiPlanRequest(configuration)
		request.Instance.Bootstrap = true
		request.TriggerKind = workflow.TriggerKindOperatorRecovery
		run, items, err := (WorkPlanner{}).PlanLokiWork(t.Context(), request)
		if err != nil {
			t.Fatalf("PlanLokiWork(%q) error = %v, want nil", configuration, err)
		}
		if got, want := run.TriggerKind, workflow.TriggerKindOperatorRecovery; got != want {
			t.Fatalf("TriggerKind = %q, want explicit %q", got, want)
		}
		if got := len(items); got != 0 {
			t.Fatalf("len(items) = %d, want 0", got)
		}
	}

	request := validLokiPlanRequest(`{"targets":[]}`)
	request.Instance.Bootstrap = true
	run, _, err := (WorkPlanner{}).PlanLokiWork(t.Context(), request)
	if err != nil {
		t.Fatalf("PlanLokiWork() error = %v, want nil", err)
	}
	if got, want := run.TriggerKind, workflow.TriggerKindBootstrap; got != want {
		t.Fatalf("TriggerKind = %q, want derived %q", got, want)
	}
	if !strings.Contains(run.RunID, ":bootstrap:") {
		t.Fatalf("RunID = %q, want bootstrap trigger component", run.RunID)
	}
}

func validLokiPlanRequest(configuration string) PlanRequest {
	observedAt := time.Date(2026, time.June, 5, 18, 30, 0, 0, time.UTC)
	return PlanRequest{
		Instance: workflow.CollectorInstance{
			InstanceID:     "loki-primary",
			CollectorKind:  scope.CollectorLoki,
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
