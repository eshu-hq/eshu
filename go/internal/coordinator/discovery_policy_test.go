// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"strings"
	"testing"
)

func TestScannerImageDiscoveryPolicyRequiresExplicitAuthority(t *testing.T) {
	for _, tc := range []struct {
		name, policy, boundary string
		wantMode               ScannerImageDiscoveryMode
		wantError              string
	}{
		{name: "unset stays disabled", wantMode: ScannerImageDiscoveryDisabled},
		{name: "local allowlist", policy: `{"mode":"local","revision":1,"source_scope_ids":["kubernetes://cluster-a"]}`, wantMode: ScannerImageDiscoveryLocal},
		{name: "hosted grant boundary", policy: `{"mode":"hosted","revision":1}`, boundary: `{"tenant_id":"tenant-a","workspace_id":"workspace-a","subject_class":"collector","policy_revision_hash":"policy-a"}`, wantMode: ScannerImageDiscoveryHosted},
		{name: "explicit disabled revision", policy: `{"mode":"disabled","revision":2}`, wantMode: ScannerImageDiscoveryDisabled},
		{name: "missing revision", policy: `{"mode":"local","source_scope_ids":["kubernetes://cluster-a"]}`, wantError: "revision"},
		{name: "local without allowlist", policy: `{"mode":"local","revision":1}`, wantError: "source_scope_ids"},
		{name: "local with hosted boundary", policy: `{"mode":"local","revision":1,"source_scope_ids":["kubernetes://cluster-a"]}`, boundary: `{"tenant_id":"tenant-a","workspace_id":"workspace-a","subject_class":"collector","policy_revision_hash":"policy-a"}`, wantError: "local"},
		{name: "hosted without boundary", policy: `{"mode":"hosted","revision":1}`, wantError: "tenant boundary"},
		{name: "hosted with local allowlist", policy: `{"mode":"hosted","revision":1,"source_scope_ids":["kubernetes://cluster-a"]}`, boundary: `{"tenant_id":"tenant-a","workspace_id":"workspace-a","subject_class":"collector","policy_revision_hash":"policy-a"}`, wantError: "source_scope_ids"},
		{name: "unknown mode", policy: `{"mode":"unknown","revision":1}`, wantError: "mode"},
		{name: "unknown key", policy: `{"mode":"local","revision":1,"source_scope_ids":["kubernetes://cluster-a"],"all_scopes":true}`, wantError: "unknown field"},
		{name: "duplicate key", policy: `{"mode":"local","mode":"hosted","revision":1}`, wantError: "duplicate"},
		{name: "duplicate source scope", policy: `{"mode":"local","revision":1,"source_scope_ids":["kubernetes://cluster-a","kubernetes://cluster-a"]}`, wantError: "duplicate"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := LoadConfig(func(key string) string {
				switch key {
				case "ESHU_WORKFLOW_COORDINATOR_SCANNER_IMAGE_DISCOVERY_POLICY_JSON":
					return tc.policy
				case "ESHU_WORKFLOW_COORDINATOR_TENANT_BOUNDARY_JSON":
					return tc.boundary
				default:
					return ""
				}
			})
			if tc.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantError) {
					t.Fatalf("LoadConfig() error = %v, want %q", err, tc.wantError)
				}
				return
			}
			if err != nil {
				t.Fatalf("LoadConfig() error = %v", err)
			}
			if cfg.ScannerImageDiscovery.Mode != tc.wantMode {
				t.Fatalf("discovery mode = %q, want %q", cfg.ScannerImageDiscovery.Mode, tc.wantMode)
			}
		})
	}
}

func TestScannerImageDiscoveryOptInRequiresServerPolicy(t *testing.T) {
	_, err := LoadConfig(func(key string) string {
		switch key {
		case "ESHU_WORKFLOW_COORDINATOR_DEPLOYMENT_MODE":
			return "active"
		case "ESHU_WORKFLOW_COORDINATOR_CLAIMS_ENABLED":
			return "true"
		case "ESHU_COLLECTOR_INSTANCES_JSON":
			return `[{"instance_id":"scanner-one","collector_kind":"scanner_worker","mode":"continuous","enabled":true,"claims_enabled":true,"configuration":{"analyzer":"image_unpacking","derive_from_running_images":true,"registry_domains":[{"authorization_domain":"public","registry":"registry.example.invalid"}]}}]`
		default:
			return ""
		}
	})
	if err == nil || !strings.Contains(err.Error(), "scanner image discovery policy") {
		t.Fatalf("derived scanner without server authority error = %v", err)
	}
}

func TestScannerImageDiscoveryPolicyRequiresExplicitRevisionWhenConstructed(t *testing.T) {
	policy := ScannerImageDiscoveryPolicy{
		Mode: ScannerImageDiscoveryLocal, SourceScopeIDs: []string{"kubernetes://cluster-a"},
	}
	if err := policy.validate(WorkflowTenantBoundary{}); err == nil || !strings.Contains(err.Error(), "explicit") {
		t.Fatalf("implicit local scanner policy error = %v, want explicit policy rejection", err)
	}
}
