// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"strings"
	"testing"
)

// Extraction PREP mirror (#8860 AC3, in-tree path): the command-surface
// contracts the extracted collector must preserve — claim-capable instance
// selection, token resolution from the environment only (`token_env`),
// tenant resolution inline or from the environment (`tenant_id`,
// `tenant_id_env`), the heartbeat-under-lease guard, and the loki claim-ID
// namespace — proved against the current in-tree config path, including
// hostile inputs.
//
// No-Observability-Change: existing eshu_dp_loki_* signals cover this
// binary; this test adds no metric, span, or pipeline stage.

func prepLokiInstancesJSON() string {
	return `[{
		"instance_id":"loki-primary",
		"collector_kind":"loki",
		"mode":"continuous",
		"enabled":true,
		"claims_enabled":true,
		"configuration":{
			"targets":[{
				"scope_id":"loki:tenant:prod",
				"instance_id":"prod",
				"base_url":"https://loki.example.test/",
				"path_prefix":"/loki",
				"token_env":"LOKI_TOKEN",
				"tenant_id_env":"LOKI_TENANT",
				"resource_limit":25,
				"max_label_values_per_label":5,
				"series_matchers":["{app=\".+\"}"],
				"stale_after":"2h",
				"enabled":true,
				"declared_ids":["series/app"],
				"observed_only_hint":true
			}]
		}
	}]`
}

func prepLokiGetenv(extra map[string]string) func(string) string {
	base := map[string]string{
		envCollectorInstances: prepLokiInstancesJSON(),
		"LOKI_TOKEN":          "resolved-token",
		"LOKI_TENANT":         "tenant-prod",
	}
	for key, value := range extra {
		base[key] = value
	}
	return func(key string) string { return base[key] }
}

func TestExtractionPrepClaimBoundaryRequiresClaimCapableInstance(t *testing.T) {
	t.Parallel()

	config, err := loadClaimedRuntimeConfig(prepLokiGetenv(nil))
	if err != nil {
		t.Fatalf("loadClaimedRuntimeConfig() error = %v, want nil", err)
	}
	if config.Instance.CollectorKind != "loki" {
		t.Errorf("CollectorKind = %q, want loki", config.Instance.CollectorKind)
	}
	if len(config.Source.Targets) != 1 || config.Source.Targets[0].ScopeID != "loki:tenant:prod" {
		t.Fatalf("Targets = %+v, want one loki:tenant:prod target", config.Source.Targets)
	}
	if config.Source.Targets[0].Token != "resolved-token" {
		t.Error("token was not resolved from the environment")
	}
	if config.Source.Targets[0].TenantID != "tenant-prod" {
		t.Error("tenant was not resolved from the environment")
	}

	// An inline tenant_id is a supported operator shape when no env name is
	// set; the recorded contract pins it alongside the env path.
	inlineTenant := strings.Replace(prepLokiInstancesJSON(), `"tenant_id_env":"LOKI_TENANT"`, `"tenant_id":"inline-tenant","tenant_id_env":""`, 1)
	inline, err := loadClaimedRuntimeConfig(prepLokiGetenv(map[string]string{
		envCollectorInstances: inlineTenant,
	}))
	if err != nil {
		t.Fatalf("loadClaimedRuntimeConfig(inline tenant) error = %v, want nil", err)
	}
	if inline.Source.Targets[0].TenantID != "inline-tenant" {
		t.Errorf("TenantID = %q, want inline-tenant", inline.Source.Targets[0].TenantID)
	}

	// A second enabled loki instance without an explicit selector fails
	// closed instead of collecting under an ambiguous identity.
	twoInstances := `[` + prepLokiInstancesJSON()[1:len(prepLokiInstancesJSON())-1] + `,{
		"instance_id":"loki-secondary",
		"collector_kind":"loki",
		"mode":"continuous",
		"enabled":true,
		"claims_enabled":true,
		"configuration":{"targets":[]}
	}]`
	if _, err := loadClaimedRuntimeConfig(prepLokiGetenv(map[string]string{
		envCollectorInstances: twoInstances,
	})); err == nil {
		t.Error("loadClaimedRuntimeConfig(ambiguous instances) = nil, want explicit-selector rejection")
	} else if !strings.Contains(err.Error(), envCollectorInstanceID) {
		t.Errorf("ambiguous-instance error = %q, want %s guidance", err, envCollectorInstanceID)
	}

	// A non-claim instance is not a valid collection identity.
	nonClaim := strings.Replace(prepLokiInstancesJSON(), `"claims_enabled":true`, `"claims_enabled":false`, 1)
	if _, err := loadClaimedRuntimeConfig(prepLokiGetenv(map[string]string{
		envCollectorInstances: nonClaim,
	})); err == nil {
		t.Error("loadClaimedRuntimeConfig(non-claim instance) = nil, want claim-capable rejection")
	}
}

func TestExtractionPrepHeartbeatGuardAndClaimNamespace(t *testing.T) {
	t.Parallel()

	badHeartbeat := prepLokiGetenv(map[string]string{
		envHeartbeatInterval: "60s",
		envClaimLeaseTTL:     "30s",
	})
	if _, err := loadClaimedRuntimeConfig(badHeartbeat); err == nil {
		t.Error("loadClaimedRuntimeConfig(heartbeat >= lease) = nil, want guard rejection")
	}

	first, second := newClaimID(), newClaimID()
	for _, id := range []string{first, second} {
		if !strings.HasPrefix(id, "loki-claim-") {
			t.Errorf("newClaimID() = %q, want loki-claim- namespace", id)
		}
	}
	if first == second {
		t.Error("newClaimID() repeated without uniqueness")
	}
}
