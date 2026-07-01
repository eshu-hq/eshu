// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/secretsiam"
)

const runServiceFullName = "//run.googleapis.com/projects/demo-project/locations/us-central1/services/api-service"

func runServiceContext(data string) ExtractContext {
	return ExtractContext{
		FullResourceName: runServiceFullName,
		AssetType:        assetTypeRunService,
		ProjectID:        "demo-project",
		Data:             json.RawMessage(data),
	}
}

func containsStringSlice(haystack []string, needle string) bool {
	for _, v := range haystack {
		if v == needle {
			return true
		}
	}
	return false
}

func TestRunServiceExtractorIsRegistered(t *testing.T) {
	if _, ok := lookupAssetExtractor(assetTypeRunService); !ok {
		t.Fatalf("expected %q extractor to self-register via init()", assetTypeRunService)
	}
}

func TestExtractRunServiceFullResource(t *testing.T) {
	const data = `{
		"name": "projects/demo-project/locations/us-central1/services/api-service",
		"ingress": "INGRESS_TRAFFIC_INTERNAL_ONLY",
		"latestReadyRevision": "projects/demo-project/locations/us-central1/services/api-service/revisions/api-service-00003-abc",
		"createTime": "2024-06-01T00:00:00Z",
		"template": {
			"serviceAccount": "runtime-sa@demo-project.iam.gserviceaccount.com",
			"executionEnvironment": "EXECUTION_ENVIRONMENT_GEN2",
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
			]
		}
	}`

	got, err := extractRunService(runServiceContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	saDigest := secretsiam.GCPServiceAccountEmailDigest("runtime-sa@demo-project.iam.gserviceaccount.com")
	if saDigest == "" {
		t.Fatalf("service account email digest must be non-empty")
	}
	wantAttrs := map[string]any{
		"ingress":                     "INGRESS_TRAFFIC_INTERNAL_ONLY",
		"execution_environment":       "EXECUTION_ENVIRONMENT_GEN2",
		"vpc_egress":                  "ALL_TRAFFIC",
		"scaling_min_instance_count":  1,
		"scaling_max_instance_count":  5,
		"latest_ready_revision":       "projects/demo-project/locations/us-central1/services/api-service/revisions/api-service-00003-abc",
		"creation_time":               "2024-06-01T00:00:00Z",
		"container_count":             1,
		"env_keys":                    []string{"DB_PASSWORD", "LOG_LEVEL"},
		"env_key_count":               2,
		"secret_mount_count":          2,
		"service_account_fingerprint": saDigest,
	}
	if !reflect.DeepEqual(got.Attributes, wantAttrs) {
		t.Fatalf("attributes mismatch:\n got %#v\nwant %#v", got.Attributes, wantAttrs)
	}

	// The runtime service account is joined via the fingerprinted-email digest
	// anchor (the IAM/trust layer owns the inbound edge); the VPC connector and
	// mounted secrets resolve as typed full-resource-name edges.
	if !containsStringSlice(got.CorrelationAnchors, saDigest) {
		t.Errorf("expected SA email digest anchor, got %#v", got.CorrelationAnchors)
	}
	for _, want := range []string{
		"//vpcaccess.googleapis.com/projects/demo-project/locations/us-central1/connectors/serverless-conn",
		"//secretmanager.googleapis.com/projects/demo-project/secrets/db-password",
		"//secretmanager.googleapis.com/projects/demo-project/secrets/app-config",
	} {
		if !containsStringSlice(got.CorrelationAnchors, want) {
			t.Errorf("missing anchor %q in %#v", want, got.CorrelationAnchors)
		}
	}

	if len(got.Relationships) != 3 {
		t.Fatalf("expected 1 connector + 2 secret edges, got %d: %#v", len(got.Relationships), got.Relationships)
	}
	assertRelationship(t, got.Relationships, relationshipTypeRunServiceUsesVPCConnector,
		"//vpcaccess.googleapis.com/projects/demo-project/locations/us-central1/connectors/serverless-conn", assetTypeVPCAccessConnector)
	assertRelationship(t, got.Relationships, relationshipTypeRunServiceMountsSecret,
		"//secretmanager.googleapis.com/projects/demo-project/secrets/db-password", secretManagerSecretAssetType)
	assertRelationship(t, got.Relationships, relationshipTypeRunServiceMountsSecret,
		"//secretmanager.googleapis.com/projects/demo-project/secrets/app-config", secretManagerSecretAssetType)
	for _, rel := range got.Relationships {
		if rel.SourceFullResourceName != runServiceFullName {
			t.Errorf("relationship source = %q, want run service full name", rel.SourceFullResourceName)
		}
		if rel.SourceAssetType != assetTypeRunService {
			t.Errorf("relationship source asset type = %q, want %q", rel.SourceAssetType, assetTypeRunService)
		}
	}
}

func TestExtractRunServiceNeverPersistsEnvValues(t *testing.T) {
	const data = `{
		"template": {
			"containers": [
				{"env": [{"name": "API_KEY", "value": "super-secret-value"}]}
			]
		}
	}`
	got, err := extractRunService(runServiceContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	blob, _ := json.Marshal(got)
	for _, token := range []string{"super-secret-value", "\"value\""} {
		if containsString(string(blob), token) {
			t.Fatalf("run service extraction leaked env value token %q: %s", token, blob)
		}
	}
	if got.Attributes["env_key_count"] != 1 {
		t.Errorf("env_key_count = %v, want 1", got.Attributes["env_key_count"])
	}
	if keys, _ := got.Attributes["env_keys"].([]string); !reflect.DeepEqual(keys, []string{"API_KEY"}) {
		t.Errorf("env_keys = %#v, want [API_KEY]", got.Attributes["env_keys"])
	}
}

func TestExtractRunServiceSecretCountStableWhenUnresolvable(t *testing.T) {
	// A bare secret id cannot be expanded to a full resource name without a
	// project, so it emits no edge — but the mounted-secret posture count must
	// still report the mount, independent of edge resolution.
	const data = `{
		"template": {
			"containers": [
				{"env": [{"name": "TOKEN", "valueSource": {"secretKeyRef": {"secret": "bare-secret"}}}]}
			]
		}
	}`
	ctx := ExtractContext{
		FullResourceName: runServiceFullName,
		AssetType:        assetTypeRunService,
		ProjectID:        "",
		Data:             json.RawMessage(data),
	}
	got, err := extractRunService(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Attributes["secret_mount_count"] != 1 {
		t.Errorf("secret_mount_count = %v, want 1", got.Attributes["secret_mount_count"])
	}
	if len(got.Relationships) != 0 {
		t.Errorf("expected no secret edge for an unresolvable bare id, got %#v", got.Relationships)
	}
}

func TestExtractRunServiceMinimal(t *testing.T) {
	const data = `{"createTime": "2023-01-15T09:30:00Z"}`
	got, err := extractRunService(runServiceContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantAttrs := map[string]any{"creation_time": "2023-01-15T09:30:00Z"}
	if !reflect.DeepEqual(got.Attributes, wantAttrs) {
		t.Fatalf("attributes mismatch:\n got %#v\nwant %#v", got.Attributes, wantAttrs)
	}
	if len(got.Relationships) != 0 {
		t.Errorf("expected no edges for a minimal service, got %#v", got.Relationships)
	}
	if len(got.CorrelationAnchors) != 0 {
		t.Errorf("expected no anchors, got %#v", got.CorrelationAnchors)
	}
}

func TestExtractRunServiceScalingZeroMin(t *testing.T) {
	// A zero minimum instance count is a meaningful scale-to-zero posture and must
	// be reported, distinct from an absent scaling block.
	const data = `{"template": {"scaling": {"minInstanceCount": 0, "maxInstanceCount": 3}}}`
	got, err := extractRunService(runServiceContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Attributes["scaling_min_instance_count"] != 0 {
		t.Errorf("scaling_min_instance_count = %v, want 0", got.Attributes["scaling_min_instance_count"])
	}
	if got.Attributes["scaling_max_instance_count"] != 3 {
		t.Errorf("scaling_max_instance_count = %v, want 3", got.Attributes["scaling_max_instance_count"])
	}
}

func TestExtractRunServiceEmptyDataYieldsNothing(t *testing.T) {
	got, err := extractRunService(runServiceContext(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Attributes) != 0 {
		t.Errorf("expected no attributes for empty data, got %#v", got.Attributes)
	}
	if len(got.Relationships) != 0 {
		t.Errorf("expected no relationships for empty data, got %#v", got.Relationships)
	}
	if len(got.CorrelationAnchors) != 0 {
		t.Errorf("expected no anchors for empty data, got %#v", got.CorrelationAnchors)
	}
}

func TestExtractRunServiceMalformedDataErrors(t *testing.T) {
	if _, err := extractRunService(runServiceContext(`{not json`)); err == nil {
		t.Fatalf("expected an error for malformed resource data")
	}
}

func TestRunServiceConnectorFullName(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"relative connector", "projects/p/locations/l/connectors/c", "//vpcaccess.googleapis.com/projects/p/locations/l/connectors/c"},
		{"leading slash", "/projects/p/locations/l/connectors/c", "//vpcaccess.googleapis.com/projects/p/locations/l/connectors/c"},
		{"already full name", "//vpcaccess.googleapis.com/projects/p/locations/l/connectors/c", "//vpcaccess.googleapis.com/projects/p/locations/l/connectors/c"},
		{"blank", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := runServiceConnectorFullName(tc.in); got != tc.want {
				t.Errorf("runServiceConnectorFullName(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestRunServiceSecretFullName(t *testing.T) {
	cases := []struct {
		name    string
		project string
		in      string
		want    string
	}{
		{"bare id", "demo-project", "db-password", "//secretmanager.googleapis.com/projects/demo-project/secrets/db-password"},
		{"full relative", "demo-project", "projects/other/secrets/app-config", "//secretmanager.googleapis.com/projects/other/secrets/app-config"},
		{"already full name", "demo-project", "//secretmanager.googleapis.com/projects/other/secrets/app-config", "//secretmanager.googleapis.com/projects/other/secrets/app-config"},
		{"bare id no project", "", "db-password", ""},
		{"blank", "demo-project", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := runServiceSecretFullName(tc.project, tc.in); got != tc.want {
				t.Errorf("runServiceSecretFullName(%q,%q) = %q, want %q", tc.project, tc.in, got, tc.want)
			}
		})
	}
}
