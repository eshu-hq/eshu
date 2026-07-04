// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/secretsiam"
)

const cloudBuildTriggerFullNameConst = "//cloudbuild.googleapis.com/projects/demo-project/locations/us-central1/triggers/trigger-123"

func cloudBuildTriggerContext(data string) ExtractContext {
	return ExtractContext{
		FullResourceName: cloudBuildTriggerFullNameConst,
		AssetType:        assetTypeCloudBuildTrigger,
		ProjectID:        "demo-project",
		Data:             json.RawMessage(data),
	}
}

func TestCloudBuildTriggerExtractorIsRegistered(t *testing.T) {
	if _, ok := lookupAssetExtractor(assetTypeCloudBuildTrigger); !ok {
		t.Fatalf("expected %q extractor to self-register via init()", assetTypeCloudBuildTrigger)
	}
}

func TestExtractCloudBuildTriggerRepoSource(t *testing.T) {
	const data = `{
		"id": "trigger-123",
		"name": "deploy-on-push",
		"createTime": "2026-06-26T11:00:00Z",
		"disabled": false,
		"filename": "cloudbuild.yaml",
		"eventType": "REPO",
		"includeBuildLogs": "INCLUDE_BUILD_LOGS_WITH_STATUS",
		"serviceAccount": "projects/demo-project/serviceAccounts/trigger-sa@demo-project.iam.gserviceaccount.com",
		"triggerTemplate": {"projectId": "demo-project", "repoName": "my-repo", "branchName": "main"},
		"includedFiles": ["src/**"],
		"ignoredFiles": ["docs/**", "*.md"],
		"tags": ["nightly", "release"],
		"approvalConfig": {"approvalRequired": true},
		"substitutions": {"_DEPLOY_SECRET": "should-not-leak-value"}
	}`

	got, err := extractCloudBuildTrigger(cloudBuildTriggerContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	saDigest := secretsiam.GCPServiceAccountEmailDigest("trigger-sa@demo-project.iam.gserviceaccount.com")
	if saDigest == "" {
		t.Fatalf("service account digest must be non-empty")
	}
	wantAttrs := map[string]any{
		"name":                        "deploy-on-push",
		"disabled":                    false,
		"creation_time":               "2026-06-26T11:00:00Z",
		"filename":                    "cloudbuild.yaml",
		"event_type":                  "REPO",
		"source_type":                 "repo",
		"include_build_logs":          true,
		"approval_required":           true,
		"included_files_count":        1,
		"ignored_files_count":         2,
		"tags_count":                  2,
		"service_account_fingerprint": saDigest,
	}
	if !reflect.DeepEqual(got.Attributes, wantAttrs) {
		t.Fatalf("attributes mismatch:\n got %#v\nwant %#v", got.Attributes, wantAttrs)
	}

	const repo = "//sourcerepo.googleapis.com/projects/demo-project/repos/my-repo"
	for _, want := range []string{saDigest, repo} {
		if !containsStringSlice(got.CorrelationAnchors, want) {
			t.Errorf("missing anchor %q in %#v", want, got.CorrelationAnchors)
		}
	}
	if len(got.Relationships) != 1 {
		t.Fatalf("expected one source-repo edge, got %d: %#v", len(got.Relationships), got.Relationships)
	}
	assertRelationship(t, got.Relationships, relationshipTypeTriggerSourceRepo, repo, assetTypeSourceRepo)
	for _, rel := range got.Relationships {
		if rel.SourceFullResourceName != cloudBuildTriggerFullNameConst {
			t.Errorf("relationship source = %q", rel.SourceFullResourceName)
		}
		if rel.SourceAssetType != assetTypeCloudBuildTrigger {
			t.Errorf("relationship source asset type = %q", rel.SourceAssetType)
		}
	}
}

func TestExtractCloudBuildTriggerRepoSourceDefaultProject(t *testing.T) {
	// triggerTemplate.projectId is optional and defaults to the trigger's own
	// project; the source-repo edge must still resolve using the trigger's project.
	const data = `{"triggerTemplate": {"repoName": "my-repo", "branchName": "main"}}`
	got, err := extractCloudBuildTrigger(cloudBuildTriggerContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertRelationship(t, got.Relationships, relationshipTypeTriggerSourceRepo,
		"//sourcerepo.googleapis.com/projects/demo-project/repos/my-repo", assetTypeSourceRepo)
}

func TestExtractCloudBuildTriggerGitHubSource(t *testing.T) {
	const data = `{
		"id": "trigger-gh",
		"eventType": "REPO",
		"github": {
			"owner": "eshu-hq",
			"name": "eshu",
			"push": {"branch": "^main$"}
		}
	}`
	got, err := extractCloudBuildTrigger(cloudBuildTriggerContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Attributes["source_type"] != "github" {
		t.Errorf("source_type = %v, want github", got.Attributes["source_type"])
	}
	// GitHub is not a CAI-resolvable asset type in this graph; no edge is
	// emitted, and no repo URL or owner/name pair is persisted as an
	// unbounded free-text value.
	if len(got.Relationships) != 0 {
		t.Errorf("expected no relationships for a github-sourced trigger, got %#v", got.Relationships)
	}
}

func TestExtractCloudBuildTriggerPubsubSource(t *testing.T) {
	const data = `{
		"id": "trigger-pubsub",
		"eventType": "PUBSUB",
		"pubsubConfig": {"subscription": "projects/demo-project/subscriptions/sub-1", "topic": "projects/demo-project/topics/deploy-topic"}
	}`
	got, err := extractCloudBuildTrigger(cloudBuildTriggerContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Attributes["source_type"] != "pubsub" {
		t.Errorf("source_type = %v, want pubsub", got.Attributes["source_type"])
	}
}

func TestExtractCloudBuildTriggerWebhookSource(t *testing.T) {
	const data = `{"id": "trigger-webhook", "eventType": "WEBHOOK"}`
	got, err := extractCloudBuildTrigger(cloudBuildTriggerContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Attributes["source_type"] != "webhook" {
		t.Errorf("source_type = %v, want webhook", got.Attributes["source_type"])
	}
}

func TestExtractCloudBuildTriggerManualSource(t *testing.T) {
	const data = `{"id": "trigger-manual", "eventType": "MANUAL"}`
	got, err := extractCloudBuildTrigger(cloudBuildTriggerContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Attributes["source_type"] != "manual" {
		t.Errorf("source_type = %v, want manual", got.Attributes["source_type"])
	}
}

func TestExtractCloudBuildTriggerNeverPersistsSubstitutions(t *testing.T) {
	const data = `{"id": "trigger-x", "substitutions": {"_DEPLOY_SECRET": "should-not-leak-value"}}`
	got, err := extractCloudBuildTrigger(cloudBuildTriggerContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	blob, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, token := range []string{"should-not-leak-value", "_DEPLOY_SECRET", "substitutions"} {
		if containsString(string(blob), token) {
			t.Fatalf("trigger extraction leaked substitution token %q: %s", token, blob)
		}
	}
}

func TestExtractCloudBuildTriggerTagsReducedToCount(t *testing.T) {
	// tags is free-form user text, unlike the shared labels map, and is never
	// fingerprinted by the collector's label path; only a bounded count may
	// leave the parser, never the tag strings themselves.
	const data = `{"id": "trigger-tags", "tags": ["do-not-leak-tag-one", "do-not-leak-tag-two"]}`
	got, err := extractCloudBuildTrigger(cloudBuildTriggerContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Attributes["tags_count"] != 2 {
		t.Errorf("tags_count = %v, want 2", got.Attributes["tags_count"])
	}
	blob, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, token := range []string{"do-not-leak-tag-one", "do-not-leak-tag-two"} {
		if containsString(string(blob), token) {
			t.Fatalf("trigger extraction leaked raw tag value %q: %s", token, blob)
		}
	}
}

func TestExtractCloudBuildTriggerNeverPersistsWebhookOrFilterDetail(t *testing.T) {
	const data = `{
		"id": "trigger-y",
		"webhookConfig": {"secret": "projects/demo-project/secrets/whsecret/versions/1", "state": "OK"},
		"filter": "sensitive-cel-expression-detail"
	}`
	got, err := extractCloudBuildTrigger(cloudBuildTriggerContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	blob, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, token := range []string{"whsecret", "sensitive-cel-expression-detail"} {
		if containsString(string(blob), token) {
			t.Fatalf("trigger extraction leaked forbidden token %q: %s", token, blob)
		}
	}
}

func TestExtractCloudBuildTriggerEmptyDataYieldsNothing(t *testing.T) {
	got, err := extractCloudBuildTrigger(cloudBuildTriggerContext(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Attributes) != 0 || len(got.Relationships) != 0 || len(got.CorrelationAnchors) != 0 {
		t.Errorf("expected empty extraction, got %#v", got)
	}
}

func TestExtractCloudBuildTriggerMalformedDataErrors(t *testing.T) {
	if _, err := extractCloudBuildTrigger(cloudBuildTriggerContext(`{not json`)); err == nil {
		t.Fatalf("expected an error for malformed resource data")
	}
}

func TestExtractCloudBuildTriggerAbsentBooleanNotFabricated(t *testing.T) {
	// disabled/approval_required must be absent-vs-present-0/false: if the API
	// omits disabled and approvalConfig, the attribute must not appear at all.
	got, err := extractCloudBuildTrigger(cloudBuildTriggerContext(`{"id": "trigger-z"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := got.Attributes["disabled"]; ok {
		t.Errorf("disabled must be absent when the API omits it, got %#v", got.Attributes["disabled"])
	}
	if _, ok := got.Attributes["approval_required"]; ok {
		t.Errorf("approval_required must be absent when approvalConfig is omitted, got %#v", got.Attributes["approval_required"])
	}
}
