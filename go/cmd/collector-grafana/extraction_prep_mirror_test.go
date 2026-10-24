// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"strings"
	"testing"
)

// Extraction PREP mirror (#8857 AC3, in-tree path): the command-surface
// contracts the extracted collector must preserve — claim-capable instance
// selection, credential resolution from the environment only, the
// heartbeat-under-lease guard, and the grafana claim-ID namespace — proved
// against the current in-tree config path, including hostile inputs.
//
// No-Observability-Change: existing eshu_dp_grafana_* signals cover this
// binary; this test adds no metric, span, or pipeline stage.

func extractionPrepInstancesJSON() string {
	return `[{
		"instance_id":"grafana-primary",
		"collector_kind":"grafana",
		"mode":"continuous",
		"enabled":true,
		"claims_enabled":true,
		"configuration":{
			"targets":[{
				"provider":"grafana",
				"scope_id":"grafana:prod",
				"instance_id":"prod",
				"base_url":"https://grafana.example.test/",
				"token_env":"GRAFANA_TOKEN",
				"resource_limit":25,
				"stale_after":"2h",
				"enabled":true,
				"declared_uids":["dashboards/checkout"],
				"observed_only_hint":true
			}]
		}
	}]`
}

func extractionPrepGetenv(extra map[string]string) func(string) string {
	base := map[string]string{
		envCollectorInstances: extractionPrepInstancesJSON(),
		"GRAFANA_TOKEN":       "resolved-token",
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
	if config.Instance.CollectorKind != "grafana" {
		t.Errorf("CollectorKind = %q, want grafana", config.Instance.CollectorKind)
	}
	if len(config.Source.Targets) != 1 || config.Source.Targets[0].ScopeID != "grafana:prod" {
		t.Fatalf("Targets = %+v, want one grafana:prod target", config.Source.Targets)
	}
	if config.Source.Targets[0].Token != "resolved-token" {
		t.Error("token was not resolved from the environment")
	}

	// A second enabled grafana instance without an explicit selector fails
	// closed instead of collecting under an ambiguous identity.
	twoInstances := `[` + extractionPrepInstancesJSON()[1:len(extractionPrepInstancesJSON())-1] + `,{
		"instance_id":"grafana-secondary",
		"collector_kind":"grafana",
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
		envCollectorInstanceID: "grafana-secondary",
	}))
	if err != nil {
		t.Fatalf("loadClaimedRuntimeConfig(selector) error = %v, want nil", err)
	}
	if selected.Instance.InstanceID != "grafana-secondary" {
		t.Errorf("InstanceID = %q, want grafana-secondary", selected.Instance.InstanceID)
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
		if !strings.HasPrefix(id, "grafana-claim-") {
			t.Errorf("claim ID %q missing grafana-claim- namespace prefix", id)
		}
	}
	if first == second {
		t.Error("consecutive claim IDs identical; claim identity must be unique per acquisition")
	}
}
