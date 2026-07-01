// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/secretsiam"
)

const (
	cloudBuildFullName    = "//cloudbuild.googleapis.com/projects/demo-project/locations/us-central1/builds/build-abc"
	cloudBuildImageDigest = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
)

func cloudBuildContext(data string) ExtractContext {
	return ExtractContext{
		FullResourceName: cloudBuildFullName,
		AssetType:        assetTypeCloudBuild,
		ProjectID:        "demo-project",
		Data:             json.RawMessage(data),
	}
}

func TestCloudBuildExtractorIsRegistered(t *testing.T) {
	if _, ok := lookupAssetExtractor(assetTypeCloudBuild); !ok {
		t.Fatalf("expected %q extractor to self-register via init()", assetTypeCloudBuild)
	}
}

func TestExtractCloudBuildFullResource(t *testing.T) {
	const data = `{
		"id": "build-abc",
		"status": "SUCCESS",
		"createTime": "2026-06-26T11:50:00Z",
		"finishTime": "2026-06-26T11:55:00Z",
		"serviceAccount": "projects/demo-project/serviceAccounts/build-sa@demo-project.iam.gserviceaccount.com",
		"buildTriggerId": "trigger-123",
		"logUrl": "https://console.cloud.google.com/cloud-build/builds/build-abc?project=demo-project",
		"source": {"repoSource": {"projectId": "demo-project", "repoName": "my-repo", "branchName": "main"}},
		"images": ["us-docker.pkg.dev/demo-project/team/app@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"],
		"results": {"images": [{"name": "us-docker.pkg.dev/demo-project/team/app", "digest": "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}]},
		"substitutions": {"_DEPLOY_SECRET": "should-not-leak-value"}
	}`

	got, err := extractCloudBuild(cloudBuildContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	saDigest := secretsiam.GCPServiceAccountEmailDigest("build-sa@demo-project.iam.gserviceaccount.com")
	if saDigest == "" {
		t.Fatalf("service account digest must be non-empty")
	}
	wantAttrs := map[string]any{
		"status":                      "SUCCESS",
		"creation_time":               "2026-06-26T11:50:00Z",
		"finish_time":                 "2026-06-26T11:55:00Z",
		"log_url_host":                "console.cloud.google.com",
		"source_type":                 "repo",
		"image_count":                 1,
		"service_account_fingerprint": saDigest,
	}
	if !reflect.DeepEqual(got.Attributes, wantAttrs) {
		t.Fatalf("attributes mismatch:\n got %#v\nwant %#v", got.Attributes, wantAttrs)
	}

	const trigger = "//cloudbuild.googleapis.com/projects/demo-project/locations/us-central1/triggers/trigger-123"
	const repo = "//sourcerepo.googleapis.com/projects/demo-project/repos/my-repo"
	for _, want := range []string{saDigest, trigger, repo, cloudBuildImageDigest} {
		if !containsStringSlice(got.CorrelationAnchors, want) {
			t.Errorf("missing anchor %q in %#v", want, got.CorrelationAnchors)
		}
	}

	if len(got.Relationships) != 2 {
		t.Fatalf("expected trigger + source-repo edges, got %d: %#v", len(got.Relationships), got.Relationships)
	}
	assertRelationship(t, got.Relationships, relationshipTypeBuildTriggeredBy, trigger, assetTypeCloudBuildTrigger)
	assertRelationship(t, got.Relationships, relationshipTypeBuildSourceRepo, repo, assetTypeSourceRepo)
	for _, rel := range got.Relationships {
		if rel.SourceFullResourceName != cloudBuildFullName {
			t.Errorf("relationship source = %q", rel.SourceFullResourceName)
		}
		if rel.SourceAssetType != assetTypeCloudBuild {
			t.Errorf("relationship source asset type = %q", rel.SourceAssetType)
		}
	}
}

func TestExtractCloudBuildStorageSource(t *testing.T) {
	const data = `{
		"id": "build-min",
		"status": "QUEUED",
		"source": {"storageSource": {"bucket": "gcf-build-src", "object": "source-should-not-leak.tgz"}}
	}`
	got, err := extractCloudBuild(cloudBuildContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Attributes["source_type"] != "storage" {
		t.Errorf("source_type = %v, want storage", got.Attributes["source_type"])
	}
	assertRelationship(t, got.Relationships, relationshipTypeBuildSourceBucket,
		"//storage.googleapis.com/projects/_/buckets/gcf-build-src", assetTypeStorageBucket)
	blob, _ := json.Marshal(got)
	if containsString(string(blob), "source-should-not-leak.tgz") {
		t.Fatalf("source object path leaked: %s", blob)
	}
}

func TestExtractCloudBuildRepoSourceDefaultProject(t *testing.T) {
	// repoSource.projectId is optional and defaults to the build's own project;
	// the source-repo edge must still resolve using the build's project.
	const data = `{"status": "SUCCESS", "source": {"repoSource": {"repoName": "my-repo", "branchName": "main"}}}`
	got, err := extractCloudBuild(cloudBuildContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertRelationship(t, got.Relationships, relationshipTypeBuildSourceRepo,
		"//sourcerepo.googleapis.com/projects/demo-project/repos/my-repo", assetTypeSourceRepo)
}

func TestExtractCloudBuildNeverPersistsSubstitutions(t *testing.T) {
	const data = `{"status": "SUCCESS", "substitutions": {"_DEPLOY_SECRET": "should-not-leak-value"}}`
	got, err := extractCloudBuild(cloudBuildContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	blob, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, token := range []string{"should-not-leak-value", "_DEPLOY_SECRET", "substitutions"} {
		if containsString(string(blob), token) {
			t.Fatalf("build extraction leaked substitution token %q: %s", token, blob)
		}
	}
}

func TestExtractCloudBuildEmptyDataYieldsNothing(t *testing.T) {
	got, err := extractCloudBuild(cloudBuildContext(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Attributes) != 0 || len(got.Relationships) != 0 || len(got.CorrelationAnchors) != 0 {
		t.Errorf("expected empty extraction, got %#v", got)
	}
}

func TestExtractCloudBuildMalformedDataErrors(t *testing.T) {
	if _, err := extractCloudBuild(cloudBuildContext(`{not json`)); err == nil {
		t.Fatalf("expected an error for malformed resource data")
	}
}

func TestCloudBuildServiceAccountEmail(t *testing.T) {
	cases := []struct{ in, want string }{
		{"projects/p/serviceAccounts/sa@p.iam.gserviceaccount.com", "sa@p.iam.gserviceaccount.com"},
		{"sa@p.iam.gserviceaccount.com", "sa@p.iam.gserviceaccount.com"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := cloudBuildServiceAccountEmail(tc.in); got != tc.want {
			t.Errorf("cloudBuildServiceAccountEmail(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestCloudBuildLogURLHost(t *testing.T) {
	cases := []struct{ in, want string }{
		{"https://console.cloud.google.com/cloud-build/builds/x?project=p", "console.cloud.google.com"},
		{"http://host/path", "host"},
		{"https://user:pass@host.example.com:443/path", "host.example.com"},
		{"https://HOST.EXAMPLE.com/path", "host.example.com"},
		{"not-a-url", ""},
		{"", ""},
	}
	for _, tc := range cases {
		if got := cloudBuildLogURLHost(tc.in); got != tc.want {
			t.Errorf("cloudBuildLogURLHost(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
