// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpplanner

import (
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func validEdgeGCPRequest(observedAt time.Time, configuration string) PlanRequest {
	return PlanRequest{
		Instance:   testGCPInstance("gcp-primary", observedAt, configuration),
		ObservedAt: observedAt,
		PlanKey:    "continuous-20260701T120000Z",
	}
}

func TestWorkPlannerRejectsInvalidRequestAndConfiguration(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.July, 1, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		mutate  func(*PlanRequest)
		wantErr string
	}{
		{
			name: "missing instance id",
			mutate: func(request *PlanRequest) {
				request.Instance.InstanceID = ""
			},
			wantErr: "instance_id",
		},
		{
			name: "wrong collector kind",
			mutate: func(request *PlanRequest) {
				request.Instance.CollectorKind = scope.CollectorGit
			},
			wantErr: `gcp planner requires collector_kind "gcp"`,
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
			name: "blank plan key",
			mutate: func(request *PlanRequest) {
				request.PlanKey = "   "
			},
			wantErr: "plan_key must not be blank",
		},
		{
			name: "malformed configuration",
			mutate: func(request *PlanRequest) {
				request.Instance.Configuration = `{"scopes":`
			},
			wantErr: "configuration must be valid JSON",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			request := validEdgeGCPRequest(observedAt, testGCPConfigWithTwoEnabledScopes())
			test.mutate(&request)
			_, _, err := (WorkPlanner{}).PlanGCPWork(t.Context(), request)
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("PlanGCPWork() error = %v, want containing %q", err, test.wantErr)
			}
		})
	}
}

func TestWorkPlannerRejectsInvalidScopeConfiguration(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.July, 1, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name          string
		configuration string
		wantErr       string
	}{
		{
			name:          "live collection not enabled",
			configuration: `{"scopes":[{"enabled":true,"parent_scope_kind":"project","parent_scope_id":"project-alpha","credential_ref":"credential-handle"}]}`,
			wantErr:       "live_collection_enabled=true",
		},
		{
			name:          "no enabled scopes",
			configuration: `{"live_collection_enabled":true,"scopes":[{"enabled":false,"parent_scope_kind":"project","parent_scope_id":"project-alpha","credential_ref":"credential-handle"}]}`,
			wantErr:       "requires at least one enabled scope",
		},
		{
			name: "duplicate scope id",
			configuration: `{"live_collection_enabled":true,"scopes":[
				{"enabled":true,"scope_id":"dup","parent_scope_kind":"project","parent_scope_id":"project-alpha","credential_ref":"credential-a"},
				{"enabled":true,"scope_id":"dup","parent_scope_kind":"project","parent_scope_id":"project-beta","credential_ref":"credential-b"}
			]}`,
			wantErr: "duplicate scope_id",
		},
		{
			name:          "invalid parent scope kind",
			configuration: `{"live_collection_enabled":true,"scopes":[{"enabled":true,"parent_scope_kind":"region","parent_scope_id":"project-alpha","credential_ref":"credential-handle"}]}`,
			wantErr:       "invalid parent_scope_kind",
		},
		{
			name:          "missing parent scope id",
			configuration: `{"live_collection_enabled":true,"scopes":[{"enabled":true,"parent_scope_kind":"project","credential_ref":"credential-handle"}]}`,
			wantErr:       "parent_scope_id is required",
		},
		{
			name:          "parent scope id contains path delimiter",
			configuration: `{"live_collection_enabled":true,"scopes":[{"enabled":true,"parent_scope_kind":"project","parent_scope_id":"project/alpha","credential_ref":"credential-handle"}]}`,
			wantErr:       "unsupported path or query delimiters",
		},
		{
			name:          "missing credential ref",
			configuration: `{"live_collection_enabled":true,"scopes":[{"enabled":true,"parent_scope_kind":"project","parent_scope_id":"project-alpha"}]}`,
			wantErr:       "credential_ref is required",
		},
		{
			name:          "invalid asset type family",
			configuration: `{"live_collection_enabled":true,"scopes":[{"enabled":true,"parent_scope_kind":"project","parent_scope_id":"project-alpha","asset_type_family":"compute/instances","credential_ref":"credential-handle"}]}`,
			wantErr:       "asset_type_family is invalid",
		},
		{
			name:          "invalid content family",
			configuration: `{"live_collection_enabled":true,"scopes":[{"enabled":true,"parent_scope_kind":"project","parent_scope_id":"project-alpha","content_family":"resource#bad","credential_ref":"credential-handle"}]}`,
			wantErr:       "content_family is invalid",
		},
		{
			name:          "invalid location bucket",
			configuration: `{"live_collection_enabled":true,"scopes":[{"enabled":true,"parent_scope_kind":"project","parent_scope_id":"project-alpha","location_bucket":"us?east","credential_ref":"credential-handle"}]}`,
			wantErr:       "location_bucket is invalid",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			request := validEdgeGCPRequest(observedAt, test.configuration)
			if _, _, err := (WorkPlanner{}).PlanGCPWork(t.Context(), request); err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("PlanGCPWork() error = %v, want containing %q", err, test.wantErr)
			}
		})
	}
}

func TestWithDefaultsFillsOptionalFieldsAndDerivesScopeID(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.July, 1, 12, 0, 0, 0, time.UTC)
	configuration := `{"live_collection_enabled":true,"scopes":[{"enabled":true,"parent_scope_kind":"project","parent_scope_id":"project-alpha","credential_ref":"credential-handle"}]}`
	_, items, err := (WorkPlanner{}).PlanGCPWork(t.Context(), PlanRequest{
		Instance:   testGCPInstance("gcp-primary", observedAt, configuration),
		ObservedAt: observedAt,
		PlanKey:    "continuous-20260701T120000Z",
	})
	if err != nil {
		t.Fatalf("PlanGCPWork() error = %v, want nil (missing optional fields must default)", err)
	}
	if got, want := len(items), 1; got != want {
		t.Fatalf("len(items) = %d, want %d", got, want)
	}
	if items[0].ScopeID == "" {
		t.Fatal("ScopeID is empty, want a scope id derived from defaulted fields")
	}
	for _, want := range []string{"mixed", "resource", "global"} {
		if !strings.Contains(items[0].ScopeID, want) {
			t.Fatalf("ScopeID = %q, want it to reflect default %q", items[0].ScopeID, want)
		}
	}
}

func TestFilterGCPTargetsByScopeIDsTrimsAndIgnoresBlanks(t *testing.T) {
	t.Parallel()

	targets := []gcpScopeConfiguration{
		{ScopeID: "alpha"},
		{ScopeID: "beta"},
	}
	filtered := filterGCPTargetsByScopeIDs(targets, []string{"  alpha  ", "", "   ", "alpha"})
	if got, want := len(filtered), 1; got != want {
		t.Fatalf("len(filtered) = %d, want %d", got, want)
	}
	if filtered[0].ScopeID != "alpha" {
		t.Fatalf("filtered[0].ScopeID = %q, want %q", filtered[0].ScopeID, "alpha")
	}
}

func TestEnabledScopesSurfacesSameValidationAsPlanGCPWork(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		configuration string
		wantErr       string
	}{
		{
			name:          "malformed json",
			configuration: `{"scopes":`,
			wantErr:       "decode GCP collector configuration",
		},
		{
			name:          "live collection not enabled",
			configuration: `{"scopes":[{"enabled":true,"parent_scope_kind":"project","parent_scope_id":"project-alpha","credential_ref":"credential-handle"}]}`,
			wantErr:       "live_collection_enabled=true",
		},
		{
			name:          "no enabled scopes",
			configuration: `{"live_collection_enabled":true,"scopes":[]}`,
			wantErr:       "requires at least one enabled scope",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := EnabledScopes(test.configuration); err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("EnabledScopes() error = %v, want containing %q", err, test.wantErr)
			}
		})
	}
}

func TestEnabledScopesOmitsContentFamilyAndCredentialFields(t *testing.T) {
	t.Parallel()

	scopes, err := EnabledScopes(testGCPConfigWithTwoEnabledScopes())
	if err != nil {
		t.Fatalf("EnabledScopes() error = %v, want nil", err)
	}
	if len(scopes) == 0 {
		t.Fatal("EnabledScopes() returned no scopes, want at least one")
	}
	for _, s := range scopes {
		if s.ScopeID == "" {
			t.Fatal("ConfiguredScope.ScopeID is empty, want a derived scope id")
		}
		if s.ParentScopeKind == "" || s.ParentScopeID == "" {
			t.Fatalf("ConfiguredScope %+v missing parent scope identity", s)
		}
	}
}

func TestValidateClaimSchedulerConfigurationRejectsInvalidConfiguration(t *testing.T) {
	t.Parallel()

	desired := workflow.DesiredCollectorInstance{
		InstanceID:    "gcp-primary",
		CollectorKind: scope.CollectorGCP,
		Mode:          workflow.CollectorModeContinuous,
		Enabled:       true,
		ClaimsEnabled: true,
		Configuration: `{"live_collection_enabled":true,"scopes":[{"enabled":false}]}`,
	}
	err := ValidateClaimSchedulerConfiguration(desired)
	if err == nil || !strings.Contains(err.Error(), "requires at least one enabled scope") {
		t.Fatalf("ValidateClaimSchedulerConfiguration() error = %v, want containing %q", err, "requires at least one enabled scope")
	}
	if !strings.Contains(err.Error(), desired.InstanceID) {
		t.Fatalf("ValidateClaimSchedulerConfiguration() error = %v, want it to name the offending instance %q", err, desired.InstanceID)
	}
}
