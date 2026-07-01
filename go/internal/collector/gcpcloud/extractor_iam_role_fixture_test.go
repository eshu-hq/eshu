// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

// TestIAMRoleOfflineFixtureEndToEnd exercises the offline assets.list fixture for
// custom IAM Role through parse -> normalize -> attribute extraction ->
// generation -> envelope, proving the redaction-safe typed-depth attributes reach
// durable facts without any live GCP call, and that the raw etag never lands on a
// fact.
func TestIAMRoleOfflineFixtureEndToEnd(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "assets_list_iam_role.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	page, err := ParseAssetsListPage(raw)
	if err != nil {
		t.Fatalf("parse fixture page: %v", err)
	}
	if len(page.Resources) != 2 {
		t.Fatalf("expected 2 resources, got %d", len(page.Resources))
	}

	gen := NewGeneration(attributesTestBoundary(), redact.Key{})
	if err := gen.AddPage(page.Resources); err != nil {
		t.Fatalf("add page: %v", err)
	}
	envelopes, err := gen.Build()
	if err != nil {
		t.Fatalf("build generation: %v", err)
	}

	resourceCount := 0
	var deployerAttrs map[string]any
	for _, env := range envelopes {
		if env.FactKind != facts.GCPCloudResourceFactKind {
			continue
		}
		resourceCount++
		if env.Payload["full_resource_name"] == iamRoleFullName {
			deployerAttrs, _ = env.Payload["attributes"].(map[string]any)
		}
	}

	if resourceCount != 2 {
		t.Errorf("gcp_cloud_resource facts = %d, want 2", resourceCount)
	}
	if deployerAttrs == nil {
		t.Fatalf("customDeployer role carried no attributes")
	}
	if deployerAttrs["title"] != "Custom Deployer" {
		t.Errorf("customDeployer title = %v, want Custom Deployer", deployerAttrs["title"])
	}
	if deployerAttrs["grants_privilege_escalation"] != true {
		t.Errorf("customDeployer grants_privilege_escalation = %v, want true", deployerAttrs["grants_privilege_escalation"])
	}

	// The raw etag must never reach any fact.
	blob, err := json.Marshal(envelopes)
	if err != nil {
		t.Fatalf("marshal envelopes: %v", err)
	}
	for _, token := range []string{"BwXhwFakeEtag=", "BwXhwOtherEtag="} {
		if containsString(string(blob), token) {
			t.Fatalf("envelope set leaked raw etag token %q", token)
		}
	}
}
