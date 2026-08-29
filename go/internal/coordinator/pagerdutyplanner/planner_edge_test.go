// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package pagerdutyplanner

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestHasConfiguredScopeUsesValidatedConfiguredTargets(t *testing.T) {
	t.Parallel()

	configuration := `{
		"targets": [{
			"provider": "pagerduty",
			"scope_id": "pagerduty:service:checkout",
			"account_id": "example",
			"token_env": "PAGERDUTY_TOKEN",
			"incident_limit": 25,
			"log_entry_limit": 25,
			"change_event_limit": 25
		}]
	}`
	tests := []struct {
		name          string
		configuration string
		scopeID       string
		want          bool
	}{
		{name: "exact", configuration: configuration, scopeID: "pagerduty:service:checkout", want: true},
		{name: "trimmed", configuration: configuration, scopeID: " pagerduty:service:checkout ", want: true},
		{name: "unconfigured", configuration: configuration, scopeID: "pagerduty:service:payments"},
		{name: "blank", configuration: configuration, scopeID: "   "},
		{name: "invalid configuration", configuration: "{", scopeID: "pagerduty:service:checkout"},
		{
			name:          "semantically invalid matching target",
			configuration: `{"targets":[{"provider":"pagerduty","scope_id":"pagerduty:service:checkout","account_id":"example"}]}`,
			scopeID:       "pagerduty:service:checkout",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := HasConfiguredScope(test.configuration, test.scopeID); got != test.want {
				t.Fatalf("HasConfiguredScope() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestWorkPlannerValidatesAllTargetsBeforeScopeFiltering(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.June, 6, 18, 30, 0, 0, time.UTC)
	configuration := `{
		"targets": [{
			"provider": "pagerduty",
			"scope_id": "pagerduty:service:selected",
			"account_id": "example",
			"token_env": "PAGERDUTY_TOKEN",
			"incident_limit": 25,
			"log_entry_limit": 25,
			"change_event_limit": 25
		}, {
			"provider": "pagerduty",
			"scope_id": " pagerduty:service:duplicate ",
			"account_id": "example",
			"token_env": "PAGERDUTY_TOKEN",
			"incident_limit": 25,
			"log_entry_limit": 25,
			"change_event_limit": 25
		}, {
			"provider": "pagerduty",
			"scope_id": "pagerduty:service:duplicate",
			"account_id": "example",
			"token_env": "PAGERDUTY_TOKEN",
			"incident_limit": 25,
			"log_entry_limit": 25,
			"change_event_limit": 25
		}]
	}`
	request := PlanRequest{
		Instance:   validPagerDutyInstance(observedAt, configuration),
		ObservedAt: observedAt,
		PlanKey:    "schedule-20260606T183000Z",
		ScopeIDs:   []string{"pagerduty:service:selected"},
	}

	_, _, err := (WorkPlanner{}).PlanPagerDutyWork(t.Context(), request)
	if err == nil || !strings.Contains(err.Error(), `duplicate pagerduty target scope_id "pagerduty:service:duplicate"`) {
		t.Fatalf("PlanPagerDutyWork() error = %v, want duplicate unselected target rejection", err)
	}
}

func TestWorkPlannerRejectsInvalidRequestsAndUnselectedTargets(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.June, 6, 18, 30, 0, 0, time.UTC)
	validRequest := PlanRequest{
		Instance:   validPagerDutyInstance(observedAt, testPagerDutyConfig()),
		ObservedAt: observedAt,
		PlanKey:    "schedule-20260606T183000Z",
	}
	invalidUnselectedConfiguration := `{
		"targets": [{
			"provider": "pagerduty",
			"scope_id": "pagerduty:service:selected",
			"account_id": "example",
			"token_env": "PAGERDUTY_TOKEN"
		}, {
			"provider": "pagerduty",
			"scope_id": "pagerduty:service:invalid",
			"account_id": "example"
		}]
	}`
	tests := []struct {
		name    string
		mutate  func(*PlanRequest)
		wantErr string
	}{
		{
			name: "invalid instance",
			mutate: func(request *PlanRequest) {
				request.Instance.InstanceID = ""
			},
			wantErr: "instance_id",
		},
		{
			name: "wrong collector kind",
			mutate: func(request *PlanRequest) {
				request.Instance.CollectorKind = scope.CollectorGit
				request.Instance.Configuration = "{}"
			},
			wantErr: `requires collector_kind "pagerduty"`,
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
			name: "zero observation time",
			mutate: func(request *PlanRequest) {
				request.ObservedAt = time.Time{}
			},
			wantErr: "observed_at must not be zero",
		},
		{
			name: "invalid trigger kind",
			mutate: func(request *PlanRequest) {
				request.TriggerKind = workflow.TriggerKind("invalid")
			},
			wantErr: `unknown workflow trigger kind "invalid"`,
		},
		{
			name: "malformed configuration",
			mutate: func(request *PlanRequest) {
				request.Instance.Configuration = "{"
			},
			wantErr: "configuration must be valid JSON",
		},
		{
			name: "empty configuration",
			mutate: func(request *PlanRequest) {
				request.Instance.Configuration = ""
			},
			wantErr: "requires targets",
		},
		{
			name: "invalid unselected target",
			mutate: func(request *PlanRequest) {
				request.Instance.Configuration = invalidUnselectedConfiguration
				request.ScopeIDs = []string{"pagerduty:service:selected"}
			},
			wantErr: "targets[1]: token_env is required",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			request := validRequest
			test.mutate(&request)
			_, _, err := (WorkPlanner{}).PlanPagerDutyWork(t.Context(), request)
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("PlanPagerDutyWork() error = %v, want substring %q", err, test.wantErr)
			}
		})
	}
}

func TestWorkPlannerRejectsEveryInvalidUnselectedTargetField(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.June, 6, 18, 30, 0, 0, time.UTC)
	const selectedTarget = `{"provider":"pagerduty","scope_id":"pagerduty:service:selected","account_id":"example","token_env":"PAGERDUTY_TOKEN"}`
	tests := []struct {
		name    string
		target  string
		wantErr string
	}{
		{name: "provider", target: `{"provider":"opsgenie","scope_id":"pagerduty:service:invalid","account_id":"example","token_env":"TOKEN"}`, wantErr: `targets[1]: unsupported pagerduty provider "opsgenie"`},
		{name: "scope", target: `{"provider":"pagerduty","scope_id":" ","account_id":"example","token_env":"TOKEN"}`, wantErr: "targets[1]: scope_id is required"},
		{name: "account", target: `{"provider":"pagerduty","scope_id":"pagerduty:service:invalid","account_id":" ","token_env":"TOKEN"}`, wantErr: "targets[1]: account_id is required"},
		{name: "incident limit", target: `{"provider":"pagerduty","scope_id":"pagerduty:service:invalid","account_id":"example","token_env":"TOKEN","incident_limit":101}`, wantErr: "targets[1]: incident_limit must be between 0 and 100"},
		{name: "log entry limit", target: `{"provider":"pagerduty","scope_id":"pagerduty:service:invalid","account_id":"example","token_env":"TOKEN","log_entry_limit":-1}`, wantErr: "targets[1]: log_entry_limit must be between 0 and 100"},
		{name: "change event limit", target: `{"provider":"pagerduty","scope_id":"pagerduty:service:invalid","account_id":"example","token_env":"TOKEN","change_event_limit":101}`, wantErr: "targets[1]: change_event_limit must be between 0 and 100"},
		{name: "config resource limit", target: `{"provider":"pagerduty","scope_id":"pagerduty:service:invalid","account_id":"example","token_env":"TOKEN","config_resource_limit":101}`, wantErr: "targets[1]: config_resource_limit must be between 0 and 100"},
		{name: "pagination pages", target: `{"provider":"pagerduty","scope_id":"pagerduty:service:invalid","account_id":"example","token_env":"TOKEN","pagination_max_pages":101}`, wantErr: "targets[1]: pagination_max_pages must be between 0 and 100"},
		{name: "pagination records", target: `{"provider":"pagerduty","scope_id":"pagerduty:service:invalid","account_id":"example","token_env":"TOKEN","pagination_max_records":5001}`, wantErr: "targets[1]: pagination_max_records must be between 0 and 5000"},
		{name: "lookback", target: `{"provider":"pagerduty","scope_id":"pagerduty:service:invalid","account_id":"example","token_env":"TOKEN","incident_lookback":"forever"}`, wantErr: "targets[1]: parse incident_lookback"},
		{name: "api URL", target: `{"provider":"pagerduty","scope_id":"pagerduty:service:invalid","account_id":"example","token_env":"TOKEN","api_base_url":"http://api.pagerduty.com"}`, wantErr: "targets[1]: api_base_url must use https"},
		{name: "source URL", target: `{"provider":"pagerduty","scope_id":"pagerduty:service:invalid","account_id":"example","token_env":"TOKEN","source_uri":"ftp://example.com/incidents"}`, wantErr: "targets[1]: source_uri must use http or https"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			request := PlanRequest{
				Instance:   validPagerDutyInstance(observedAt, `{"targets":[`+selectedTarget+`,`+test.target+`]}`),
				ObservedAt: observedAt,
				PlanKey:    "schedule-20260606T183000Z",
				ScopeIDs:   []string{"pagerduty:service:selected"},
			}
			_, _, err := (WorkPlanner{}).PlanPagerDutyWork(t.Context(), request)
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("PlanPagerDutyWork() error = %v, want substring %q", err, test.wantErr)
			}
		})
	}
}

func TestWorkPlannerReturnsPendingRunForEmptyScopeSelection(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.June, 6, 18, 30, 0, 0, time.UTC)
	tests := []struct {
		name     string
		scopeIDs []string
	}{
		{name: "unmatched", scopeIDs: []string{"pagerduty:service:missing"}},
		{name: "nonempty blank-only", scopeIDs: []string{" ", "\t"}},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			run, items, err := (WorkPlanner{}).PlanPagerDutyWork(t.Context(), PlanRequest{
				Instance:   validPagerDutyInstance(observedAt, testPagerDutyConfig()),
				ObservedAt: observedAt,
				PlanKey:    "schedule-20260606T183000Z",
				ScopeIDs:   test.scopeIDs,
			})
			if err != nil {
				t.Fatalf("PlanPagerDutyWork() error = %v, want nil", err)
			}
			wantRun := workflow.Run{
				RunID:              "pagerduty:pagerduty-primary:schedule:schedule-20260606T183000Z",
				TriggerKind:        workflow.TriggerKindSchedule,
				Status:             workflow.RunStatusCollectionPending,
				RequestedScopeSet:  `{"collector_instance_id":"pagerduty-primary","targets":[]}`,
				RequestedCollector: string(scope.CollectorPagerDuty),
				CreatedAt:          observedAt,
				UpdatedAt:          observedAt,
				FinishedAt:         time.Time{},
			}
			if !reflect.DeepEqual(run, wantRun) {
				t.Fatalf("run = %#v, want %#v", run, wantRun)
			}
			if len(items) != 0 {
				t.Fatalf("len(items) = %d, want 0", len(items))
			}
		})
	}
}

func TestWorkPlannerPreservesTriggerPrecedenceAndPlanKeyGrammar(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.June, 6, 18, 30, 0, 0, time.UTC)
	const validPlanKey = "plan-20260606T183000Z"
	tests := []struct {
		name        string
		bootstrap   bool
		triggerKind workflow.TriggerKind
		planKey     string
		wantTrigger workflow.TriggerKind
		wantRunID   string
		wantErr     string
	}{
		{
			name:        "scheduled fallback",
			planKey:     validPlanKey,
			wantTrigger: workflow.TriggerKindSchedule,
			wantRunID:   "pagerduty:pagerduty-primary:schedule:" + validPlanKey,
		},
		{
			name:        "bootstrap fallback",
			bootstrap:   true,
			planKey:     validPlanKey,
			wantTrigger: workflow.TriggerKindBootstrap,
			wantRunID:   "pagerduty:pagerduty-primary:bootstrap:" + validPlanKey,
		},
		{
			name:        "webhook overrides bootstrap",
			bootstrap:   true,
			triggerKind: workflow.TriggerKindWebhook,
			planKey:     validPlanKey,
			wantTrigger: workflow.TriggerKindWebhook,
			wantRunID:   "pagerduty:pagerduty-primary:webhook:" + validPlanKey,
		},
		{
			name:    "unsafe plan key",
			planKey: "contains whitespace",
			wantErr: "plan_key",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			instance := validPagerDutyInstance(observedAt, testPagerDutyConfig())
			instance.Bootstrap = test.bootstrap
			run, items, err := (WorkPlanner{}).PlanPagerDutyWork(t.Context(), PlanRequest{
				Instance:    instance,
				ObservedAt:  observedAt,
				PlanKey:     test.planKey,
				TriggerKind: test.triggerKind,
			})
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("PlanPagerDutyWork() error = %v, want substring %q", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("PlanPagerDutyWork() error = %v, want nil", err)
			}
			if got := run.TriggerKind; got != test.wantTrigger {
				t.Fatalf("TriggerKind = %q, want %q", got, test.wantTrigger)
			}
			if got := run.RunID; got != test.wantRunID {
				t.Fatalf("RunID = %q, want %q", got, test.wantRunID)
			}
			if len(items) != 1 {
				t.Fatalf("len(items) = %d, want 1", len(items))
			}
		})
	}
}
