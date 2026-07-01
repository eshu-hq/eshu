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
	runRevisionFullName    = "//run.googleapis.com/projects/demo-project/locations/us-central1/services/api-service/revisions/api-service-00003-abc"
	runRevisionParentSvc   = "//run.googleapis.com/projects/demo-project/locations/us-central1/services/api-service"
	runRevisionImageRef    = "us-docker.pkg.dev/demo-project/team/api@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	runRevisionImageDigest = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
)

func runRevisionContext(data string) ExtractContext {
	return ExtractContext{
		FullResourceName: runRevisionFullName,
		AssetType:        assetTypeRunRevision,
		ProjectID:        "demo-project",
		Data:             json.RawMessage(data),
	}
}

func TestRunRevisionExtractorIsRegistered(t *testing.T) {
	if _, ok := lookupAssetExtractor(assetTypeRunRevision); !ok {
		t.Fatalf("expected %q extractor to self-register via init()", assetTypeRunRevision)
	}
}

func TestExtractRunRevisionFullResource(t *testing.T) {
	const data = `{
		"name": "projects/demo-project/locations/us-central1/services/api-service/revisions/api-service-00003-abc",
		"service": "api-service",
		"serviceAccount": "runtime-sa@demo-project.iam.gserviceaccount.com",
		"executionEnvironment": "EXECUTION_ENVIRONMENT_GEN2",
		"createTime": "2024-06-01T00:00:00Z",
		"scaling": {"minInstanceCount": 1, "maxInstanceCount": 5},
		"vpcAccess": {"connector": "projects/demo-project/locations/us-central1/connectors/serverless-conn", "egress": "ALL_TRAFFIC"},
		"containers": [
			{
				"name": "api",
				"image": "us-docker.pkg.dev/demo-project/team/api@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
				"env": [
					{"name": "LOG_LEVEL", "value": "info-should-not-leak"},
					{"name": "DB_PASSWORD", "valueSource": {"secretKeyRef": {"secret": "db-password", "version": "latest"}}}
				]
			}
		],
		"volumes": [
			{"name": "config", "secret": {"secret": "projects/demo-project/secrets/app-config"}}
		],
		"conditions": [{"type": "Ready", "state": "CONDITION_SUCCEEDED"}]
	}`

	got, err := extractRunRevision(runRevisionContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	saDigest := secretsiam.GCPServiceAccountEmailDigest("runtime-sa@demo-project.iam.gserviceaccount.com")
	if saDigest == "" {
		t.Fatalf("service account email digest must be non-empty")
	}
	wantAttrs := map[string]any{
		"execution_environment":       "EXECUTION_ENVIRONMENT_GEN2",
		"vpc_egress":                  "ALL_TRAFFIC",
		"scaling_min_instance_count":  1,
		"scaling_max_instance_count":  5,
		"creation_time":               "2024-06-01T00:00:00Z",
		"container_count":             1,
		"container_image":             runRevisionImageRef,
		"container_image_digest":      runRevisionImageDigest,
		"env_keys":                    []string{"DB_PASSWORD", "LOG_LEVEL"},
		"env_key_count":               2,
		"secret_mount_count":          2,
		"ready_condition_state":       "CONDITION_SUCCEEDED",
		"service_account_fingerprint": saDigest,
	}
	if !reflect.DeepEqual(got.Attributes, wantAttrs) {
		t.Fatalf("attributes mismatch:\n got %#v\nwant %#v", got.Attributes, wantAttrs)
	}

	for _, want := range []string{
		saDigest,
		runRevisionParentSvc,
		"//vpcaccess.googleapis.com/projects/demo-project/locations/us-central1/connectors/serverless-conn",
		"//secretmanager.googleapis.com/projects/demo-project/secrets/db-password",
		"//secretmanager.googleapis.com/projects/demo-project/secrets/app-config",
		runRevisionImageDigest,
	} {
		if !containsStringSlice(got.CorrelationAnchors, want) {
			t.Errorf("missing anchor %q in %#v", want, got.CorrelationAnchors)
		}
	}

	if len(got.Relationships) != 4 {
		t.Fatalf("expected parent-service + connector + 2 secret edges, got %d: %#v", len(got.Relationships), got.Relationships)
	}
	assertRelationship(t, got.Relationships, relationshipTypeRevisionOfService, runRevisionParentSvc, assetTypeRunService)
	assertRelationship(t, got.Relationships, relationshipTypeRevisionUsesVPCConnector,
		"//vpcaccess.googleapis.com/projects/demo-project/locations/us-central1/connectors/serverless-conn", assetTypeVPCAccessConnector)
	assertRelationship(t, got.Relationships, relationshipTypeRevisionMountsSecret,
		"//secretmanager.googleapis.com/projects/demo-project/secrets/db-password", secretManagerSecretAssetType)
	assertRelationship(t, got.Relationships, relationshipTypeRevisionMountsSecret,
		"//secretmanager.googleapis.com/projects/demo-project/secrets/app-config", secretManagerSecretAssetType)
	for _, rel := range got.Relationships {
		if rel.SourceFullResourceName != runRevisionFullName {
			t.Errorf("relationship source = %q, want revision full name", rel.SourceFullResourceName)
		}
		if rel.SourceAssetType != assetTypeRunRevision {
			t.Errorf("relationship source asset type = %q, want %q", rel.SourceAssetType, assetTypeRunRevision)
		}
	}
}

func TestExtractRunRevisionNeverPersistsEnvValues(t *testing.T) {
	const data = `{
		"containers": [{"env": [{"name": "API_KEY", "value": "super-secret-value"}]}]
	}`
	got, err := extractRunRevision(runRevisionContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	blob, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal extraction: %v", err)
	}
	if containsString(string(blob), "super-secret-value") {
		t.Fatalf("run revision extraction leaked env value literal: %s", blob)
	}
	if got.Attributes["env_key_count"] != 1 {
		t.Errorf("env_key_count = %v, want 1", got.Attributes["env_key_count"])
	}
}

func TestExtractRunRevisionParentServiceFromName(t *testing.T) {
	// Even a data-less revision must resolve its parent service from its own full
	// resource name, so the revision_of_service edge is always emitted.
	got, err := extractRunRevision(runRevisionContext(`{"createTime": "2023-01-15T09:30:00Z"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Attributes["creation_time"] != "2023-01-15T09:30:00Z" {
		t.Errorf("creation_time = %v", got.Attributes["creation_time"])
	}
	if len(got.Relationships) != 1 {
		t.Fatalf("expected exactly the parent-service edge, got %#v", got.Relationships)
	}
	assertRelationship(t, got.Relationships, relationshipTypeRevisionOfService, runRevisionParentSvc, assetTypeRunService)
}

func TestExtractRunRevisionImageWithoutDigestOmitsDigest(t *testing.T) {
	const data = `{"containers": [{"image": "us-docker.pkg.dev/demo-project/team/api:2026-06-27"}]}`
	got, err := extractRunRevision(runRevisionContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Attributes["container_image"] != "us-docker.pkg.dev/demo-project/team/api:2026-06-27" {
		t.Errorf("container_image = %v", got.Attributes["container_image"])
	}
	if _, ok := got.Attributes["container_image_digest"]; ok {
		t.Errorf("tag-only image must not report a digest: %#v", got.Attributes)
	}
}

func TestExtractRunRevisionAnchorsAllContainerDigests(t *testing.T) {
	// A multi-container revision (sidecar) must anchor every digest-pinned image,
	// not just the primary container's, so sidecars still correlate.
	const data = `{"containers": [
		{"name": "api", "image": "us-docker.pkg.dev/p/t/api@sha256:1111111111111111111111111111111111111111111111111111111111111111"},
		{"name": "proxy", "image": "us-docker.pkg.dev/p/t/proxy@sha256:2222222222222222222222222222222222222222222222222222222222222222"},
		{"name": "notag", "image": "us-docker.pkg.dev/p/t/legacy:latest"}
	]}`
	got, err := extractRunRevision(runRevisionContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, want := range []string{
		"sha256:1111111111111111111111111111111111111111111111111111111111111111",
		"sha256:2222222222222222222222222222222222222222222222222222222222222222",
	} {
		if !containsStringSlice(got.CorrelationAnchors, want) {
			t.Errorf("missing image digest anchor %q in %#v", want, got.CorrelationAnchors)
		}
	}
	// The first container declaring an image drives the scalar image attributes.
	if got.Attributes["container_image_digest"] != "sha256:1111111111111111111111111111111111111111111111111111111111111111" {
		t.Errorf("container_image_digest = %v, want primary", got.Attributes["container_image_digest"])
	}
	if got.Attributes["container_count"] != 3 {
		t.Errorf("container_count = %v, want 3", got.Attributes["container_count"])
	}
}

func TestExtractRunRevisionSecretCountDedupsCrossForm(t *testing.T) {
	// The same secret referenced as a bare id (env) and its full relative form
	// (volume) is one mounted secret, so secret_mount_count must be 1, not 2.
	const data = `{
		"containers": [{"env": [{"name": "DB", "valueSource": {"secretKeyRef": {"secret": "db-password"}}}]}],
		"volumes": [{"secret": {"secret": "projects/demo-project/secrets/db-password"}}]
	}`
	got, err := extractRunRevision(runRevisionContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Attributes["secret_mount_count"] != 1 {
		t.Errorf("secret_mount_count = %v, want 1 (same secret in two forms)", got.Attributes["secret_mount_count"])
	}
}

func TestExtractRunRevisionMalformedDataErrors(t *testing.T) {
	if _, err := extractRunRevision(runRevisionContext(`{not json`)); err == nil {
		t.Fatalf("expected an error for malformed resource data")
	}
}

func TestRunRevisionParentServiceFullName(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"revision full name", runRevisionFullName, runRevisionParentSvc},
		{"no revisions segment", "//run.googleapis.com/projects/p/locations/l/services/s", ""},
		{"blank", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := runRevisionParentServiceFullName(tc.in); got != tc.want {
				t.Errorf("runRevisionParentServiceFullName(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
