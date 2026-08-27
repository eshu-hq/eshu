// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package vaultlive

import (
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
)

func TestWorkPlannerRejectsInvalidRequests(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*PlanRequest)
		want   string
	}{
		{
			name: "disabled instance",
			mutate: func(request *PlanRequest) {
				request.Instance.Enabled = false
			},
			want: "requires enabled collector instance",
		},
		{
			name: "claims disabled",
			mutate: func(request *PlanRequest) {
				request.Instance.ClaimsEnabled = false
			},
			want: "requires claim-enabled collector instance",
		},
		{
			name: "wrong collector kind",
			mutate: func(request *PlanRequest) {
				request.Instance.CollectorKind = scope.CollectorGrafana
			},
			want: "requires collector_kind",
		},
		{
			name: "zero observation time",
			mutate: func(request *PlanRequest) {
				request.ObservedAt = time.Time{}
			},
			want: "observed_at must not be zero",
		},
		{
			name: "blank plan key",
			mutate: func(request *PlanRequest) {
				request.PlanKey = "  "
			},
			want: "plan_key must not be blank",
		},
		{
			name: "path plan key",
			mutate: func(request *PlanRequest) {
				request.PlanKey = "vault/prod"
			},
			want: "must not include raw source locator material",
		},
		{
			name: "delimiter plan key",
			mutate: func(request *PlanRequest) {
				request.PlanKey = "vault:prod"
			},
			want: "unsupported character",
		},
		{
			name: "unicode plan key",
			mutate: func(request *PlanRequest) {
				request.PlanKey = "vault-prod-✓"
			},
			want: "unsupported character",
		},
		{
			name: "malformed configuration",
			mutate: func(request *PlanRequest) {
				request.Instance.Configuration = "{"
			},
			want: "configuration must be valid JSON",
		},
		{
			name: "empty target set",
			mutate: func(request *PlanRequest) {
				request.Instance.Configuration = `{"targets":[]}`
			},
			want: "requires targets",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			request := testPlanRequest()
			test.mutate(&request)

			_, _, err := (WorkPlanner{}).PlanVaultLiveWork(t.Context(), request)
			if err == nil {
				t.Fatal("PlanVaultLiveWork() error = nil, want rejection")
			}
			if !strings.Contains(err.Error(), test.want) {
				t.Fatalf("PlanVaultLiveWork() error = %q, want substring %q", err, test.want)
			}
		})
	}
}

func TestDuplicateTargetErrorDoesNotExposeRawIdentity(t *testing.T) {
	t.Parallel()

	request := testPlanRequest()
	request.Instance.Configuration = `{"targets":[` +
		`{"vault_cluster_id":"vault-secret","namespace":"namespace-secret","address":"https://one.example","token_env":"VAULT_ONE_TOKEN"},` +
		`{"vault_cluster_id":"vault-secret","namespace":"namespace-secret","address":"https://two.example","token_env":"VAULT_TWO_TOKEN"}` +
		`]}`
	_, _, err := (WorkPlanner{}).PlanVaultLiveWork(t.Context(), request)
	if err == nil {
		t.Fatal("PlanVaultLiveWork() error = nil, want duplicate-target error")
	}
	for _, raw := range []string{"vault-secret", "namespace-secret"} {
		if strings.Contains(err.Error(), raw) {
			t.Fatalf("duplicate-target error exposed raw identity %q: %v", raw, err)
		}
	}
}

func TestTargetKeyDoesNotCollideOnColon(t *testing.T) {
	t.Parallel()

	request := testPlanRequest()
	request.Instance.Configuration = `{"targets":[` +
		`{"vault_cluster_id":"vault:a","namespace":"admin","address":"https://one.example","token_env":"VAULT_ONE_TOKEN"},` +
		`{"vault_cluster_id":"vault","namespace":"a:admin","address":"https://two.example","token_env":"VAULT_TWO_TOKEN"}` +
		`]}`
	_, items, err := (WorkPlanner{}).PlanVaultLiveWork(t.Context(), request)
	if err != nil {
		t.Fatalf("PlanVaultLiveWork() error = %v, want nil for distinct targets", err)
	}
	if len(items) != 2 || items[0].ScopeID == items[1].ScopeID {
		t.Fatalf("items = %+v, want two distinct target scopes", items)
	}
}
