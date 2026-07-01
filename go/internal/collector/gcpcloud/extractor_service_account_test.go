// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/secretsiam"
)

const serviceAccountFullName = "//iam.googleapis.com/projects/demo-project/serviceAccounts/pipeline-runner@demo-project.iam.gserviceaccount.com"

const serviceAccountSampleEmail = "pipeline-runner@demo-project.iam.gserviceaccount.com"

func serviceAccountContext(data string) ExtractContext {
	return ExtractContext{
		FullResourceName: serviceAccountFullName,
		AssetType:        serviceAccountAssetType,
		ProjectID:        "demo-project",
		Data:             json.RawMessage(data),
	}
}

func TestServiceAccountExtractorIsRegistered(t *testing.T) {
	if _, ok := lookupAssetExtractor(serviceAccountAssetType); !ok {
		t.Fatalf("expected %q extractor to self-register via init()", serviceAccountAssetType)
	}
}

func TestExtractServiceAccountFullDepth(t *testing.T) {
	const data = `{
		"name": "projects/demo-project/serviceAccounts/pipeline-runner@demo-project.iam.gserviceaccount.com",
		"projectId": "demo-project",
		"uniqueId": "104567890123456789012",
		"email": "pipeline-runner@demo-project.iam.gserviceaccount.com",
		"displayName": "Pipeline Runner",
		"oauth2ClientId": "104567890123456789012",
		"disabled": false
	}`

	got, err := extractServiceAccount(serviceAccountContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	emailDigest := secretsiam.GCPServiceAccountEmailDigest(serviceAccountSampleEmail)
	wantAttrs := map[string]any{
		"unique_id":         "104567890123456789012",
		"email_fingerprint": emailDigest,
		"display_name":      "Pipeline Runner",
		"oauth2_client_id":  "104567890123456789012",
		"disabled":          false,
	}
	if !reflect.DeepEqual(got.Attributes, wantAttrs) {
		t.Fatalf("attributes mismatch:\n got %#v\nwant %#v", got.Attributes, wantAttrs)
	}

	// The fingerprinted email is the cross-source correlation anchor that closes
	// inbound trust / Workload-Identity / runs-as joins onto this resource node.
	wantAnchors := []string{emailDigest}
	if !reflect.DeepEqual(got.CorrelationAnchors, wantAnchors) {
		t.Fatalf("anchors mismatch:\n got %#v\nwant %#v", got.CorrelationAnchors, wantAnchors)
	}

	// Service-account graph edges are inbound and owned by the IAM/trust and
	// image-identity layers; the extractor derives no outbound edges from the
	// resource's own data.
	if len(got.Relationships) != 0 {
		t.Fatalf("expected no relationships, got %#v", got.Relationships)
	}
}

func TestExtractServiceAccountDisabledTrue(t *testing.T) {
	const data = `{
		"email": "locked@demo-project.iam.gserviceaccount.com",
		"disabled": true
	}`
	got, err := extractServiceAccount(serviceAccountContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Attributes["disabled"] != true {
		t.Errorf("disabled = %v, want true", got.Attributes["disabled"])
	}
}

func TestExtractServiceAccountKeyCountNeverLeaksMaterial(t *testing.T) {
	// CAI ServiceAccount blobs do not normally carry keys, but the extractor
	// surfaces a bounded key_count when a keys array is present and never lets
	// any key field reach the output.
	const data = `{
		"email": "keyed@demo-project.iam.gserviceaccount.com",
		"keys": [
			{"name": "projects/demo-project/serviceAccounts/keyed/keys/abc", "privateKeyData": "SUPER-SECRET-MATERIAL"},
			{"name": "projects/demo-project/serviceAccounts/keyed/keys/def", "privateKeyData": "ANOTHER-SECRET"}
		]
	}`
	got, err := extractServiceAccount(serviceAccountContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Attributes["key_count"] != 2 {
		t.Errorf("key_count = %v, want 2", got.Attributes["key_count"])
	}
	blob, _ := json.Marshal(got)
	for _, banned := range []string{"SUPER-SECRET-MATERIAL", "ANOTHER-SECRET", "privateKeyData", "keys/abc"} {
		if strings.Contains(string(blob), banned) {
			t.Fatalf("extraction leaked key token %q: %s", banned, blob)
		}
	}
}

func TestExtractServiceAccountNeverLeaksRawEmail(t *testing.T) {
	const data = `{"email": "pipeline-runner@demo-project.iam.gserviceaccount.com", "uniqueId": "104567890123456789012"}`
	got, err := extractServiceAccount(serviceAccountContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	blob, _ := json.Marshal(got)
	if strings.Contains(string(blob), serviceAccountSampleEmail) {
		t.Fatalf("extraction leaked raw service-account email: %s", blob)
	}
	if got.Attributes["email_fingerprint"] == "" {
		t.Errorf("expected email_fingerprint attribute when email present")
	}
}

func TestExtractServiceAccountEmailFallsBackToFullName(t *testing.T) {
	// A CAI page can omit resource.data.email while still carrying the canonical
	// .../serviceAccounts/<email> full resource name. The extractor must derive
	// the digest from the name so the cloud-resource node keeps the same anchor
	// the trust facts join on, matching gcpServiceAccountEmailForResource.
	const data = `{"uniqueId": "104567890123456789012", "disabled": false}`
	got, err := extractServiceAccount(serviceAccountContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantDigest := secretsiam.GCPServiceAccountEmailDigest(serviceAccountSampleEmail)
	if wantDigest == "" {
		t.Fatal("expected a non-empty digest for the sample email")
	}
	if got.Attributes["email_fingerprint"] != wantDigest {
		t.Errorf("email_fingerprint = %v, want digest derived from full resource name %q",
			got.Attributes["email_fingerprint"], wantDigest)
	}
	if len(got.CorrelationAnchors) != 1 || got.CorrelationAnchors[0] != wantDigest {
		t.Errorf("correlation_anchors = %#v, want [%s]", got.CorrelationAnchors, wantDigest)
	}
	// The raw email parsed from the name must never escape into the output.
	blob, _ := json.Marshal(got)
	if strings.Contains(string(blob), serviceAccountSampleEmail) {
		t.Fatalf("extraction leaked raw service-account email: %s", blob)
	}
}

func TestExtractServiceAccountEmptyData(t *testing.T) {
	// Empty data and a full resource name with no parseable email yields nothing:
	// no attributes, anchors, or relationships are fabricated.
	ctx := ExtractContext{
		FullResourceName: "//iam.googleapis.com/projects/demo-project/serviceAccounts/",
		AssetType:        serviceAccountAssetType,
		ProjectID:        "demo-project",
		Data:             json.RawMessage(`{}`),
	}
	got, err := extractServiceAccount(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Attributes) != 0 {
		t.Errorf("expected no attributes for empty data, got %#v", got.Attributes)
	}
	if len(got.CorrelationAnchors) != 0 {
		t.Errorf("expected no anchors for empty data, got %#v", got.CorrelationAnchors)
	}
	if len(got.Relationships) != 0 {
		t.Errorf("expected no relationships for empty data, got %#v", got.Relationships)
	}
}

func TestExtractServiceAccountMalformedDataErrors(t *testing.T) {
	_, err := extractServiceAccount(serviceAccountContext(`{not json`))
	if err == nil {
		t.Fatalf("expected an error for malformed resource data")
	}
}
