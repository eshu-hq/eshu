// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"strings"
	"testing"
)

// Extraction PREP mirror (#8866 AC3, in-tree path): the command-surface
// contracts the extracted collector must preserve — claim-capable instance
// selection, credential resolution from the environment only, the
// heartbeat-under-lease guard, and the security-alert claim-ID namespace —
// proved against the current in-tree config path, including hostile inputs.
//
// No-Observability-Change: existing eshu_dp_security_alert_* signals cover
// this binary; this test adds no metric, span, or pipeline stage.

func extractionPrepInstancesJSON() string {
	return `[{
		"instance_id":"security-alert-primary",
		"collector_kind":"security_alert",
		"mode":"continuous",
		"enabled":true,
		"claims_enabled":true,
		"configuration":{
			"targets":[{
				"provider":"github_dependabot",
				"scope_id":"security-alert:github:example-org/example-repo",
				"repository":"example-org/example-repo",
				"token_env":"GITHUB_TOKEN",
				"allowed_repositories":["example-org/example-repo"],
				"repository_alert_limit":25,
				"max_pages":2
			}]
		}
	}]`
}

func extractionPrepGetenv(extra map[string]string) func(string) string {
	base := map[string]string{
		envCollectorInstances: extractionPrepInstancesJSON(),
		"GITHUB_TOKEN":        "resolved-token",
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
	if config.Instance.CollectorKind != "security_alert" {
		t.Errorf("CollectorKind = %q, want security_alert", config.Instance.CollectorKind)
	}
	if len(config.Source.Targets) != 1 || config.Source.Targets[0].ScopeID != "security-alert:github:example-org/example-repo" {
		t.Fatalf("Targets = %+v, want one security-alert:github:example-org/example-repo target", config.Source.Targets)
	}
	if config.Source.Targets[0].Token != "resolved-token" {
		t.Error("token was not resolved from the environment")
	}

	// A second enabled security-alert instance without an explicit selector
	// fails closed instead of collecting under an ambiguous identity.
	doubled := `[` + extractionPrepInstancesJSON()[1:len(extractionPrepInstancesJSON())-1] + `,{
		"instance_id":"security-alert-secondary",
		"collector_kind":"security_alert",
		"mode":"continuous",
		"enabled":true,
		"claims_enabled":true,
		"configuration":{
			"targets":[{
				"provider":"github_dependabot",
				"scope_id":"security-alert:github:example-org/other-repo",
				"repository":"example-org/other-repo",
				"token_env":"GITHUB_TOKEN",
				"allowed_repositories":["example-org/other-repo"]
			}]
		}
	}]`
	if _, err := loadClaimedRuntimeConfig(extractionPrepGetenv(map[string]string{envCollectorInstances: doubled})); err == nil {
		t.Error("loadClaimedRuntimeConfig(two instances, no selector) = nil, want ambiguity rejection")
	}
	selected, err := loadClaimedRuntimeConfig(extractionPrepGetenv(map[string]string{
		envCollectorInstances:  doubled,
		envCollectorInstanceID: "security-alert-secondary",
	}))
	if err != nil {
		t.Fatalf("loadClaimedRuntimeConfig(selector) error = %v, want nil", err)
	}
	if selected.Instance.InstanceID != "security-alert-secondary" {
		t.Errorf("InstanceID = %q, want security-alert-secondary", selected.Instance.InstanceID)
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
		if !strings.HasPrefix(id, "security-alert-claim-") {
			t.Errorf("claim ID %q missing security-alert-claim- namespace prefix", id)
		}
	}
	if first == second {
		t.Error("consecutive claim IDs identical; claim identity must be unique per acquisition")
	}
}
