// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

// TestDataformRepositoryOfflineFixtureEndToEnd exercises the offline assets.list
// fixture for Dataform Repository through parse -> normalize -> attribute
// extraction -> generation -> envelope, proving the redaction-safe typed-depth
// attributes and the optional CMEK edge reach durable facts without any live GCP
// call, and that no raw git remote URL/host or service-account email lands on a
// fact.
func TestDataformRepositoryOfflineFixtureEndToEnd(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "assets_list_dataform_repository.json"))
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
	kmsEdges := 0
	var analyticsAttrs map[string]any
	for _, env := range envelopes {
		switch env.FactKind {
		case facts.GCPCloudResourceFactKind:
			resourceCount++
			if env.Payload["full_resource_name"] == dataformRepositoryFullName {
				analyticsAttrs, _ = env.Payload["attributes"].(map[string]any)
			}
		case facts.GCPCloudRelationshipFactKind:
			if stringOrEmpty(env.Payload["relationship_type"]) == relationshipTypeRepositoryEncryptedByKMSKey {
				kmsEdges++
			}
		}
	}

	if resourceCount != 2 {
		t.Errorf("gcp_cloud_resource facts = %d, want 2", resourceCount)
	}
	if analyticsAttrs == nil {
		t.Fatalf("analytics repository carried no attributes")
	}
	if analyticsAttrs["git_default_branch"] != "main" {
		t.Errorf("analytics git_default_branch = %v, want main", analyticsAttrs["git_default_branch"])
	}
	if fp, ok := analyticsAttrs["git_remote_host_fingerprint"].(string); !ok || !strings.HasPrefix(fp, "sha256:") {
		t.Errorf("analytics git_remote_host_fingerprint = %v, want a sha256: digest", analyticsAttrs["git_remote_host_fingerprint"])
	}
	if analyticsAttrs["customer_managed_encryption"] != true {
		t.Errorf("analytics customer_managed_encryption = %v, want true", analyticsAttrs["customer_managed_encryption"])
	}
	if kmsEdges != 1 {
		t.Errorf("dataform_repository_encrypted_by_kms_key edges = %d, want 1", kmsEdges)
	}

	// No raw git remote URL/host or service-account email may reach any fact.
	blob, err := json.Marshal(envelopes)
	if err != nil {
		t.Fatalf("marshal envelopes: %v", err)
	}
	for _, token := range []string{"github.com", "dataform-analytics", "dataform-runner@demo-project", "git-token"} {
		if containsString(string(blob), token) {
			t.Fatalf("envelope set leaked token %q", token)
		}
	}
}
