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

// TestCloudBuildTriggerOfflineFixtureEndToEnd exercises the offline
// assets.list fixture for Cloud Build Trigger through parse -> normalize ->
// attribute extraction -> generation -> envelope, proving the redaction-safe
// typed-depth attributes, correlation anchors, and the source-repo /
// source-repository-link edges reach durable facts without any live GCP
// call, that a Pub/Sub trigger's `sourceToBuild` never shadows its true
// `source_type`, and that no build substitution value, webhook secret
// reference, or raw service-account email ever lands on a fact.
func TestCloudBuildTriggerOfflineFixtureEndToEnd(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "assets_list_cloud_build_trigger.json"))
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
	repoEdges := 0
	repositoryLinkEdges := 0
	var repoTriggerAttrs, githubTriggerAttrs, pubsubTriggerAttrs map[string]any
	for _, env := range envelopes {
		switch env.FactKind {
		case facts.GCPCloudResourceFactKind:
			resourceCount++
			switch env.Payload["full_resource_name"] {
			case cloudBuildTriggerFullNameConst:
				repoTriggerAttrs, _ = env.Payload["attributes"].(map[string]any)
			case "//cloudbuild.googleapis.com/projects/demo-project/locations/global/triggers/trigger-gh":
				githubTriggerAttrs, _ = env.Payload["attributes"].(map[string]any)
			case "//cloudbuild.googleapis.com/projects/demo-project/locations/us-central1/triggers/trigger-pubsub-repo":
				pubsubTriggerAttrs, _ = env.Payload["attributes"].(map[string]any)
			}
		case facts.GCPCloudRelationshipFactKind:
			switch stringOrEmpty(env.Payload["relationship_type"]) {
			case relationshipTypeTriggerSourceRepo:
				repoEdges++
			case relationshipTypeTriggerSourceRepositoryLink:
				repositoryLinkEdges++
			}
		}
	}

	if resourceCount != 3 {
		t.Errorf("gcp_cloud_resource facts = %d, want 3", resourceCount)
	}
	if repoTriggerAttrs == nil {
		t.Fatalf("repo trigger carried no attributes")
	}
	if repoTriggerAttrs["source_type"] != "repo" {
		t.Errorf("source_type = %v, want repo", repoTriggerAttrs["source_type"])
	}
	if repoTriggerAttrs["disabled"] != false {
		t.Errorf("disabled = %v, want false", repoTriggerAttrs["disabled"])
	}
	if repoTriggerAttrs["tags_count"] != float64(2) && repoTriggerAttrs["tags_count"] != 2 {
		t.Errorf("tags_count = %v, want 2", repoTriggerAttrs["tags_count"])
	}
	if githubTriggerAttrs == nil {
		t.Fatalf("github trigger carried no attributes")
	}
	if githubTriggerAttrs["source_type"] != "github" {
		t.Errorf("source_type = %v, want github", githubTriggerAttrs["source_type"])
	}
	if pubsubTriggerAttrs == nil {
		t.Fatalf("pubsub+sourceToBuild trigger carried no attributes")
	}
	if pubsubTriggerAttrs["source_type"] != "pubsub" {
		t.Errorf("source_type = %v, want pubsub (sourceToBuild must not shadow the pubsub mechanism)", pubsubTriggerAttrs["source_type"])
	}
	if repoEdges != 1 {
		t.Errorf("trigger_source_repo edges = %d, want 1", repoEdges)
	}
	if repositoryLinkEdges != 1 {
		t.Errorf("trigger_source_repository_link edges = %d, want 1", repositoryLinkEdges)
	}

	blob, err := json.Marshal(envelopes)
	if err != nil {
		t.Fatalf("marshal envelopes: %v", err)
	}
	for _, token := range []string{
		"should-not-leak-value",
		"_DEPLOY_SECRET",
		"whsecret",
		"trigger-sa@demo-project.iam.gserviceaccount.com",
		"do-not-leak-tag-value",
	} {
		if containsString(string(blob), token) {
			t.Fatalf("envelope set leaked forbidden token %q", token)
		}
	}
}
