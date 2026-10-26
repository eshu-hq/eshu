// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"strings"
	"testing"
)

// Extraction PREP mirror (#8867 AC3, in-tree path): the command-surface
// contracts the extracted collector must preserve — claim-capable instance
// selection, credential resolution from the environment only (token and
// tenant travel by env reference, never inline), the
// heartbeat-under-lease guard, and the tempo claim-ID namespace — proved
// against the current in-tree config path, including hostile inputs.
//
// No-Observability-Change: existing eshu_dp_tempo_* signals cover this
// binary; this test adds no metric, span, or pipeline stage.

func extractionPrepInstancesJSON() string {
	return `[{
		"instance_id":"tempo-primary",
		"collector_kind":"tempo",
		"mode":"continuous",
		"enabled":true,
		"claims_enabled":true,
		"configuration":{
			"targets":[{
				"scope_id":"tempo:prod",
				"instance_id":"prod",
				"base_url":"https://tempo.example.test/",
				"token_env":"TEMPO_TOKEN",
				"tenant_id_env":"TEMPO_TENANT",
				"resource_limit":50,
				"tag_value_names":["resource.service.name"],
				"max_tag_values_per_tag":5,
				"stale_after":"45m",
				"enabled":true,
				"declared_ids":["tag/resource.service.name"],
				"observed_only_hint":true,
				"freshness_probe_enabled":true,
				"lookback":"2h"
			}]
		}
	}]`
}

func extractionPrepGetenv(extra map[string]string) func(string) string {
	base := map[string]string{
		envCollectorInstances: extractionPrepInstancesJSON(),
		"TEMPO_TOKEN":         "resolved-token",
		"TEMPO_TENANT":        "resolved-tenant",
	}
	for key, value := range extra {
		base[key] = value
	}
	return func(key string) string { return base[key] }
}

func TestExtractionPrepClaimBoundaryRequiresClaimCapableInstance(t *testing.T) {
	t.Parallel()

	config, err := loadClaimedRuntimeConfig(extractionPrepGetenv(nil))
	if err != nil {
		t.Fatalf("loadClaimedRuntimeConfig() error = %v, want nil", err)
	}
	if config.Instance.CollectorKind != "tempo" {
		t.Errorf("CollectorKind = %q, want tempo", config.Instance.CollectorKind)
	}
	if len(config.Source.Targets) != 1 || config.Source.Targets[0].ScopeID != "tempo:prod" {
		t.Fatalf("Targets = %+v, want one tempo:prod target", config.Source.Targets)
	}
	target := config.Source.Targets[0]
	if target.Token != "resolved-token" {
		t.Error("token was not resolved from the environment")
	}
	if target.TenantID != "resolved-tenant" {
		t.Error("tenant was not resolved from the environment")
	}

	// A second enabled tempo instance without an explicit selector fails
	// closed instead of collecting under an ambiguous identity.
	twoInstances := `[` + extractionPrepInstancesJSON()[1:len(extractionPrepInstancesJSON())-1] + `,{
		"instance_id":"tempo-secondary",
		"collector_kind":"tempo",
		"mode":"continuous",
		"enabled":true,
		"claims_enabled":true,
		"configuration":{"targets":[]}
	}]`
	if _, err := loadClaimedRuntimeConfig(extractionPrepGetenv(map[string]string{envCollectorInstances: twoInstances})); err == nil {
		t.Error("loadClaimedRuntimeConfig(two instances, no selector) = nil, want ambiguity rejection")
	}
	selected, err := loadClaimedRuntimeConfig(extractionPrepGetenv(map[string]string{
		envCollectorInstances:  twoInstances,
		envCollectorInstanceID: "tempo-secondary",
	}))
	if err != nil {
		t.Fatalf("loadClaimedRuntimeConfig(selector) error = %v, want nil", err)
	}
	if selected.Instance.InstanceID != "tempo-secondary" {
		t.Errorf("InstanceID = %q, want tempo-secondary", selected.Instance.InstanceID)
	}
}

func TestExtractionPrepCredentialResolutionFailsClosed(t *testing.T) {
	t.Parallel()

	// A token_env naming an unset variable fails closed instead of
	// emitting with an empty bearer credential.
	if _, err := loadClaimedRuntimeConfig(extractionPrepGetenv(map[string]string{"TEMPO_TOKEN": ""})); err == nil {
		t.Error("loadClaimedRuntimeConfig(unset token_env) = nil, want fail-closed rejection")
	}
	// A tenant_id_env naming an unset variable fails closed the same way.
	if _, err := loadClaimedRuntimeConfig(extractionPrepGetenv(map[string]string{"TEMPO_TENANT": ""})); err == nil {
		t.Error("loadClaimedRuntimeConfig(unset tenant_id_env) = nil, want fail-closed rejection")
	}
}

func TestExtractionPrepHeartbeatGuard(t *testing.T) {
	t.Parallel()

	if _, err := loadClaimedRuntimeConfig(extractionPrepGetenv(map[string]string{
		envClaimLeaseTTL:     "30s",
		envHeartbeatInterval: "30s",
	})); err == nil {
		t.Error("loadClaimedRuntimeConfig(heartbeat == lease) = nil, want heartbeat-under-lease rejection")
	}
	if _, err := loadClaimedRuntimeConfig(extractionPrepGetenv(map[string]string{
		envClaimLeaseTTL:     "30s",
		envHeartbeatInterval: "10s",
	})); err != nil {
		t.Errorf("loadClaimedRuntimeConfig(heartbeat < lease) error = %v, want nil", err)
	}
}

func TestExtractionPrepClaimIDNamespace(t *testing.T) {
	t.Parallel()

	first, second := newClaimID(), newClaimID()
	for _, id := range []string{first, second} {
		if !strings.HasPrefix(id, "tempo-claim-") {
			t.Errorf("claim ID %q missing tempo-claim- namespace prefix", id)
		}
	}
	if first == second {
		t.Error("consecutive claim IDs identical; claim identity must be unique per acquisition")
	}
}
