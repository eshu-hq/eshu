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
	serviceAccountKeyFullName = "//iam.googleapis.com/projects/demo-project/serviceAccounts/deployer@demo-project.iam.gserviceaccount.com/keys/abc123"
	serviceAccountKeyParent   = "//iam.googleapis.com/projects/demo-project/serviceAccounts/deployer@demo-project.iam.gserviceaccount.com"
	serviceAccountKeyEmail    = "deployer@demo-project.iam.gserviceaccount.com"
)

func serviceAccountKeyContext(data string) ExtractContext {
	return ExtractContext{
		FullResourceName: serviceAccountKeyFullName,
		AssetType:        serviceAccountKeyAssetType,
		ProjectID:        "demo-project",
		Data:             json.RawMessage(data),
	}
}

func TestServiceAccountKeyExtractorIsRegistered(t *testing.T) {
	if _, ok := lookupAssetExtractor(serviceAccountKeyAssetType); !ok {
		t.Fatalf("expected %q extractor to self-register via init()", serviceAccountKeyAssetType)
	}
}

func TestExtractServiceAccountKeyFullResource(t *testing.T) {
	const data = `{
		"name": "projects/demo-project/serviceAccounts/deployer@demo-project.iam.gserviceaccount.com/keys/abc123",
		"keyType": "USER_MANAGED",
		"keyAlgorithm": "KEY_ALG_RSA_2048",
		"keyOrigin": "GOOGLE_PROVIDED",
		"validAfterTime": "2024-06-01T00:00:00Z",
		"validBeforeTime": "2027-06-01T00:00:00Z",
		"disabled": false
	}`

	got, err := extractServiceAccountKey(serviceAccountKeyContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	emailDigest := secretsiam.GCPServiceAccountEmailDigest(serviceAccountKeyEmail)
	wantAttrs := map[string]any{
		"key_type":          "USER_MANAGED",
		"key_algorithm":     "KEY_ALG_RSA_2048",
		"key_origin":        "GOOGLE_PROVIDED",
		"valid_after_time":  "2024-06-01T00:00:00Z",
		"valid_before_time": "2027-06-01T00:00:00Z",
		"disabled":          false,
		"parent_service_account_email_fingerprint": emailDigest,
	}
	if !reflect.DeepEqual(got.Attributes, wantAttrs) {
		t.Fatalf("attributes mismatch:\n got %#v\nwant %#v", got.Attributes, wantAttrs)
	}

	if len(got.Relationships) != 1 {
		t.Fatalf("expected 1 parent SA edge, got %d: %#v", len(got.Relationships), got.Relationships)
	}
	assertRelationship(t, got.Relationships, relationshipTypeServiceAccountKeyOf,
		serviceAccountKeyParent, serviceAccountAssetType)
	rel := got.Relationships[0]
	if rel.SourceFullResourceName != serviceAccountKeyFullName {
		t.Errorf("relationship source = %q, want key full name", rel.SourceFullResourceName)
	}

	wantAnchors := []string{emailDigest}
	if !reflect.DeepEqual(got.CorrelationAnchors, wantAnchors) {
		t.Fatalf("anchors mismatch:\n got %#v\nwant %#v", got.CorrelationAnchors, wantAnchors)
	}
}

func TestExtractServiceAccountKeyDisabledPosture(t *testing.T) {
	const data = `{
		"keyType": "USER_MANAGED",
		"keyAlgorithm": "KEY_ALG_RSA_2048",
		"disabled": true
	}`
	got, err := extractServiceAccountKey(serviceAccountKeyContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Attributes["disabled"] != true {
		t.Errorf("disabled = %v, want true", got.Attributes["disabled"])
	}
}

func TestExtractServiceAccountKeyAbsentDisabledOmitted(t *testing.T) {
	// disabled is a pointer: an absent field must be omitted, distinct from a
	// present false (an active key, useful posture).
	const data = `{"keyType": "SYSTEM_MANAGED", "keyAlgorithm": "KEY_ALG_RSA_2048"}`
	got, err := extractServiceAccountKey(serviceAccountKeyContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := got.Attributes["disabled"]; ok {
		t.Errorf("absent disabled must be omitted: %#v", got.Attributes)
	}
}

// serviceAccountKeyMaterialTokens are the private/public key-material tokens that
// must never appear in any extracted output or emitted fact. Shared by the unit
// redaction test and the offline-fixture end-to-end test so the redaction
// boundary stays consistent as the list evolves.
var serviceAccountKeyMaterialTokens = []string{
	"c3VwZXItc2VjcmV0LXByaXZhdGUta2V5",
	"cHVibGljLWtleS1ib2R5",
	"privateKeyData",
	"publicKeyData",
	"privateKeyType",
}

func TestExtractServiceAccountKeyNeverPersistsKeyMaterial(t *testing.T) {
	// A CAI key asset should carry no material, but guard the boundary: a stray
	// private/public key field must never leak into the output.
	const data = `{
		"keyType": "USER_MANAGED",
		"privateKeyData": "c3VwZXItc2VjcmV0LXByaXZhdGUta2V5",
		"publicKeyData": "cHVibGljLWtleS1ib2R5",
		"privateKeyType": "TYPE_GOOGLE_CREDENTIALS_FILE"
	}`
	got, err := extractServiceAccountKey(serviceAccountKeyContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	blob, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal extraction: %v", err)
	}
	for _, token := range serviceAccountKeyMaterialTokens {
		if containsString(string(blob), token) {
			t.Fatalf("service account key extraction leaked material token %q: %s", token, blob)
		}
	}
}

func TestExtractServiceAccountKeyEmptyDataYieldsOnlyParentFingerprint(t *testing.T) {
	// Empty data still derives the parent-SA edge and anchor from the full
	// resource name, so the only attribute is the parent-email fingerprint; no
	// data-derived attributes are present.
	got, err := extractServiceAccountKey(serviceAccountKeyContext(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Attributes) != 1 {
		t.Errorf("expected only the parent-email fingerprint, got %#v", got.Attributes)
	}
	if _, ok := got.Attributes["parent_service_account_email_fingerprint"]; !ok {
		t.Errorf("parent fingerprint should derive from the full name even with empty data: %#v", got.Attributes)
	}
	if len(got.Relationships) != 1 {
		t.Errorf("expected the parent SA edge from the full name, got %#v", got.Relationships)
	}
}

func TestExtractServiceAccountKeyMalformedDataErrors(t *testing.T) {
	if _, err := extractServiceAccountKey(serviceAccountKeyContext(`{not json`)); err == nil {
		t.Fatalf("expected an error for malformed resource data")
	}
}

func TestParentServiceAccountFullName(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"key name", serviceAccountKeyFullName, serviceAccountKeyParent},
		{"no keys segment", serviceAccountKeyParent, ""},
		{"trailing keys marker, no id", serviceAccountKeyParent + "/keys/", ""},
		{"blank", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := parentServiceAccountFullName(tc.in); got != tc.want {
				t.Errorf("parentServiceAccountFullName(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
