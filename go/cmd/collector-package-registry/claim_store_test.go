// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/claim/store"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// claimStoreTestGetenv is a minimal valid package-registry claimed-runtime
// environment.
func claimStoreTestGetenv(key string) string {
	env := map[string]string{
		envCollectorInstances: `[{"instance_id":"collector-package-registry","collector_kind":"package_registry",
			"mode":"continuous","enabled":true,"claims_enabled":true,"configuration":{"targets":[{
			"provider":"jfrog","ecosystem":"generic","registry":"https://artifactory.example.com",
			"scope_id":"package-registry://jfrog/generic/team-api","packages":["team-api"],
			"package_limit":1,"version_limit":2,"document_format":"artifactory_package",
			"metadata_url":"https://artifactory.example.com/api/storage/generic/team-api",
			"bearer_token_env":"PACKAGE_TOKEN"}]}}]`,
		"PACKAGE_TOKEN": "token-123",
	}
	return env[key]
}

// TestBuildClaimedServiceSelectsClaimStoreFromEnv proves the collector wires
// its claim store through store.New: unset keeps the core Postgres
// store, a valid ESHU_WORKFLOW_CLAIM_PLANE_URL selects the claim-plane
// client, and an invalid one fails startup naming the variable (#8875).
func TestBuildClaimedServiceSelectsClaimStoreFromEnv(t *testing.T) {
	t.Parallel()
	withURL := func(raw string) func(string) string {
		return func(key string) string {
			if key == store.EnvClaimPlaneURL {
				return raw
			}
			return claimStoreTestGetenv(key)
		}
	}

	service, err := buildClaimedService(nil, withURL(""), nil, nil, nil)
	if err != nil {
		t.Fatalf("buildClaimedService(unset) error = %v, want nil", err)
	}
	if _, ok := service.ControlStore.(*postgres.WorkflowControlStore); !ok {
		t.Fatalf("unset ControlStore = %T, want *postgres.WorkflowControlStore", service.ControlStore)
	}
	if fenced, ok := service.Committer.(interface{ ClaimPlaneFenced() bool }); ok && fenced.ClaimPlaneFenced() {
		t.Fatalf("core Committer = %T fences on the claim plane, want the in-tree fence (#9376)", service.Committer)
	}

	service, err = buildClaimedService(nil, withURL("http://eshu-workflow-claim-plane:8080"), nil, nil, nil)
	if err != nil {
		t.Fatalf("buildClaimedService(claim plane) error = %v, want nil", err)
	}
	if _, ok := service.ControlStore.(*workflow.ClaimPlaneClient); !ok {
		t.Fatalf("claim-plane ControlStore = %T, want *workflow.ClaimPlaneClient", service.ControlStore)
	}
	if fenced, ok := service.Committer.(interface{ ClaimPlaneFenced() bool }); !ok || !fenced.ClaimPlaneFenced() {
		t.Fatalf("claim-plane Committer = %T, want one that fences commits on the claim plane (#9376)", service.Committer)
	}

	_, err = buildClaimedService(nil, withURL("ftp://claim-plane"), nil, nil, nil)
	if err == nil || !strings.Contains(err.Error(), store.EnvClaimPlaneURL) {
		t.Fatalf("buildClaimedService(invalid) error = %v, want one naming %s", err, store.EnvClaimPlaneURL)
	}
}
