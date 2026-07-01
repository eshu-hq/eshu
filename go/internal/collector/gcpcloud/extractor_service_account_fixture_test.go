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

// TestServiceAccountOfflineFixtureEndToEnd exercises the offline assets.list
// fixture for IAM ServiceAccount through parse -> normalize -> attribute
// extraction -> generation -> envelope, proving the bounded typed-depth
// attributes and fingerprinted-email correlation anchor reach the durable facts
// without any live GCP call and without leaking a raw service-account email.
func TestServiceAccountOfflineFixtureEndToEnd(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "assets_list_service_account.json"))
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

	const pipelineRunnerName = "//iam.googleapis.com/projects/demo-project/serviceAccounts/pipeline-runner@demo-project.iam.gserviceaccount.com"
	const noDataEmailName = "//iam.googleapis.com/projects/demo-project/serviceAccounts/no-data-email@demo-project.iam.gserviceaccount.com"
	resourceCount := 0
	var pipelineAttrs map[string]any
	var pipelineAnchors []string
	var noDataEmailAttrs map[string]any
	var noDataEmailAnchors []string
	for _, env := range envelopes {
		if env.FactKind != facts.GCPCloudResourceFactKind {
			continue
		}
		resourceCount++
		// The typed-depth output (attributes + correlation anchors) must carry
		// only the fingerprinted email, never the raw address. The full resource
		// name legitimately embeds the email as the provider's canonical identity
		// for exact reducer joins, so the leak check is scoped to the extractor's
		// own output rather than the whole payload.
		blob, err := json.Marshal(map[string]any{
			"attributes":          env.Payload["attributes"],
			"correlation_anchors": env.Payload["correlation_anchors"],
		})
		if err != nil {
			t.Fatalf("marshal typed-depth output: %v", err)
		}
		if strings.Contains(string(blob), "@demo-project.iam.gserviceaccount.com") {
			t.Fatalf("typed-depth output leaked a raw service-account email: %s", blob)
		}
		if env.Payload["full_resource_name"] == pipelineRunnerName {
			pipelineAttrs, _ = env.Payload["attributes"].(map[string]any)
			pipelineAnchors, _ = env.Payload["correlation_anchors"].([]string)
		}
		if env.Payload["full_resource_name"] == noDataEmailName {
			noDataEmailAttrs, _ = env.Payload["attributes"].(map[string]any)
			noDataEmailAnchors, _ = env.Payload["correlation_anchors"].([]string)
		}
	}

	if resourceCount != 3 {
		t.Errorf("gcp_cloud_resource facts = %d, want 3", resourceCount)
	}
	if pipelineAttrs == nil {
		t.Fatalf("pipeline-runner service account carried no attributes")
	}
	if pipelineAttrs["unique_id"] != "104567890123456789012" {
		t.Errorf("unique_id = %v, want 104567890123456789012", pipelineAttrs["unique_id"])
	}
	if pipelineAttrs["disabled"] != false {
		t.Errorf("disabled = %v, want false", pipelineAttrs["disabled"])
	}
	fp, ok := pipelineAttrs["email_fingerprint"].(string)
	if !ok || !strings.HasPrefix(fp, "sha256:") {
		t.Errorf("email_fingerprint = %v, want sha256: digest", pipelineAttrs["email_fingerprint"])
	}
	if len(pipelineAnchors) != 1 || pipelineAnchors[0] != fp {
		t.Errorf("correlation_anchors = %#v, want [%s]", pipelineAnchors, fp)
	}

	// The email-absent service account derives its digest anchor from the full
	// resource name so trust facts can still join onto its cloud-resource node.
	if noDataEmailAttrs == nil {
		t.Fatalf("email-absent service account carried no attributes")
	}
	derivedFP, ok := noDataEmailAttrs["email_fingerprint"].(string)
	if !ok || !strings.HasPrefix(derivedFP, "sha256:") {
		t.Errorf("email-absent email_fingerprint = %v, want sha256: digest derived from name",
			noDataEmailAttrs["email_fingerprint"])
	}
	if len(noDataEmailAnchors) != 1 || noDataEmailAnchors[0] != derivedFP {
		t.Errorf("email-absent correlation_anchors = %#v, want [%s]", noDataEmailAnchors, derivedFP)
	}
}
