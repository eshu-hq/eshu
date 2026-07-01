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

// TestWorkloadIdentityPoolOfflineFixtureEndToEnd exercises the offline
// assets.list fixture for a Workload Identity Pool and its AWS + OIDC providers
// through parse -> normalize -> attribute extraction -> generation -> envelope,
// proving the redaction-safe typed-depth attributes and the provider -> pool
// edges reach durable facts without any live GCP call, and that no OIDC/SAML key
// or metadata material ever lands on a fact.
func TestWorkloadIdentityPoolOfflineFixtureEndToEnd(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "assets_list_workload_identity_pool.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	page, err := ParseAssetsListPage(raw)
	if err != nil {
		t.Fatalf("parse fixture page: %v", err)
	}
	if len(page.Resources) != 3 {
		t.Fatalf("expected 3 resources, got %d", len(page.Resources))
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
	poolEdges := 0
	var poolAttrs, awsProviderAttrs map[string]any
	for _, env := range envelopes {
		switch env.FactKind {
		case facts.GCPCloudResourceFactKind:
			resourceCount++
			switch env.Payload["full_resource_name"] {
			case workloadIdentityPoolFullName:
				poolAttrs, _ = env.Payload["attributes"].(map[string]any)
			case wifProviderFullName:
				awsProviderAttrs, _ = env.Payload["attributes"].(map[string]any)
			}
		case facts.GCPCloudRelationshipFactKind:
			if stringOrEmpty(env.Payload["relationship_type"]) == relationshipTypeWIFProviderOfPool {
				poolEdges++
			}
		}
	}

	if resourceCount != 3 {
		t.Errorf("gcp_cloud_resource facts = %d, want 3", resourceCount)
	}
	if poolAttrs == nil || poolAttrs["state"] != "ACTIVE" {
		t.Errorf("pool attrs missing/incorrect: %#v", poolAttrs)
	}
	if awsProviderAttrs == nil || awsProviderAttrs["aws_account_id"] != "123456789012" {
		t.Errorf("aws provider attrs missing/incorrect: %#v", awsProviderAttrs)
	}
	if poolEdges != 2 {
		t.Errorf("workload_identity_provider_of_pool edges = %d, want 2", poolEdges)
	}

	blob, err := json.Marshal(envelopes)
	if err != nil {
		t.Fatalf("marshal envelopes: %v", err)
	}
	for _, token := range []string{"jwksJson", "idpMetadataXml"} {
		if containsString(string(blob), token) {
			t.Fatalf("envelope set leaked provider key/metadata token %q", token)
		}
	}
}
