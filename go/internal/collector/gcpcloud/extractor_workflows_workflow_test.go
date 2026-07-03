// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/secretsiam"
)

const workflowsWorkflowFullName = "//workflows.googleapis.com/projects/demo-project/locations/us-central1/workflows/order-pipeline"

func workflowsWorkflowContext(data string) ExtractContext {
	return ExtractContext{
		FullResourceName: workflowsWorkflowFullName,
		AssetType:        assetTypeWorkflowsWorkflow,
		ProjectID:        "demo-project",
		Data:             json.RawMessage(data),
	}
}

func TestWorkflowsWorkflowExtractorIsRegistered(t *testing.T) {
	if _, ok := lookupAssetExtractor(assetTypeWorkflowsWorkflow); !ok {
		t.Fatalf("expected %q extractor to self-register via init()", assetTypeWorkflowsWorkflow)
	}
}

func TestExtractWorkflowsWorkflowActiveWithServiceAccountAndCMEK(t *testing.T) {
	const data = `{
		"name": "projects/demo-project/locations/us-central1/workflows/order-pipeline",
		"state": "ACTIVE",
		"revisionId": "000001-abc",
		"createTime": "2024-06-01T00:00:00Z",
		"updateTime": "2024-06-02T00:00:00Z",
		"revisionCreateTime": "2024-06-02T00:00:00Z",
		"serviceAccount": "projects/demo-project/serviceAccounts/workflow-runner@demo-project.iam.gserviceaccount.com",
		"callLogLevel": "LOG_ERRORS_ONLY",
		"executionHistoryLevel": "EXECUTION_HISTORY_DETAILED",
		"cryptoKeyName": "projects/demo-project/locations/us-central1/keyRings/rk/cryptoKeys/workflow-key",
		"sourceContents": "main:\n  steps:\n  - init:\n      assign:\n      - secret: \"do-not-leak-me\"\n"
	}`

	got, err := extractWorkflowsWorkflow(workflowsWorkflowContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantFingerprint := secretsiam.GCPServiceAccountEmailDigest("workflow-runner@demo-project.iam.gserviceaccount.com")
	if wantFingerprint == "" {
		t.Fatalf("expected non-empty service account fingerprint for test setup")
	}

	wantAttrs := map[string]any{
		"state":                       "ACTIVE",
		"revision_id":                 "000001-abc",
		"call_log_level":              "LOG_ERRORS_ONLY",
		"execution_history_level":     "EXECUTION_HISTORY_DETAILED",
		"creation_time":               "2024-06-01T00:00:00Z",
		"update_time":                 "2024-06-02T00:00:00Z",
		"revision_create_time":        "2024-06-02T00:00:00Z",
		"source_contents_present":     true,
		"crypto_key_name":             "projects/demo-project/locations/us-central1/keyRings/rk/cryptoKeys/workflow-key",
		"service_account_fingerprint": wantFingerprint,
	}
	for k, want := range wantAttrs {
		got, ok := got.Attributes[k]
		if !ok {
			t.Errorf("missing attribute %q", k)
			continue
		}
		if got != want {
			t.Errorf("attribute %q = %v, want %v", k, got, want)
		}
	}
	if len(got.Attributes) != len(wantAttrs) {
		t.Fatalf("attributes mismatch: got %#v want keys %#v", got.Attributes, wantAttrs)
	}

	wantKMSName := "//cloudkms.googleapis.com/projects/demo-project/locations/us-central1/keyRings/rk/cryptoKeys/workflow-key"
	assertRelationship(t, got.Relationships, relationshipTypeWorkflowsWorkflowEncryptedByKMSKey, wantKMSName, assetTypeKMSCryptoKey)
	if len(got.Relationships) != 1 {
		t.Fatalf("expected exactly 1 relationship (kms), got %d: %#v", len(got.Relationships), got.Relationships)
	}

	foundKMSAnchor := false
	foundSAAnchor := false
	for _, a := range got.CorrelationAnchors {
		if a == wantKMSName {
			foundKMSAnchor = true
		}
		if a == wantFingerprint {
			foundSAAnchor = true
		}
	}
	if !foundKMSAnchor {
		t.Errorf("expected KMS anchor %q in %#v", wantKMSName, got.CorrelationAnchors)
	}
	if !foundSAAnchor {
		t.Errorf("expected service account fingerprint anchor %q in %#v", wantFingerprint, got.CorrelationAnchors)
	}

	// Never leak the workflow source body or the raw service-account email.
	blob, _ := json.Marshal(got)
	for _, banned := range []string{"do-not-leak-me", "workflow-runner@demo-project.iam.gserviceaccount.com", "secret:"} {
		if containsString(string(blob), banned) {
			t.Fatalf("extraction leaked sourceContents/raw-identity token %q: %s", banned, blob)
		}
	}
}

func TestExtractWorkflowsWorkflowMinimalNoCMEKNoServiceAccount(t *testing.T) {
	const data = `{
		"name": "projects/demo-project/locations/us-central1/workflows/minimal",
		"state": "STATE_UNSPECIFIED",
		"revisionId": "000001-aaa"
	}`

	got, err := extractWorkflowsWorkflow(workflowsWorkflowContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Attributes["state"] != "STATE_UNSPECIFIED" {
		t.Errorf("state = %v, want STATE_UNSPECIFIED", got.Attributes["state"])
	}
	if _, ok := got.Attributes["service_account_fingerprint"]; ok {
		t.Errorf("expected no service_account_fingerprint attribute when serviceAccount is absent")
	}
	if _, ok := got.Attributes["crypto_key_name"]; ok {
		t.Errorf("expected no crypto_key_name attribute when cryptoKeyName is absent")
	}
	if _, ok := got.Attributes["source_contents_present"]; ok {
		t.Errorf("expected no source_contents_present attribute when sourceContents is absent")
	}
	if len(got.Relationships) != 0 {
		t.Errorf("expected no relationships without a CMEK key, got %#v", got.Relationships)
	}
}

func TestExtractWorkflowsWorkflowKMSKeyAlreadyPrefixedIsNotDoublePrefixed(t *testing.T) {
	const data = `{
		"state": "ACTIVE",
		"cryptoKeyName": "//cloudkms.googleapis.com/projects/demo-project/locations/us-central1/keyRings/rk/cryptoKeys/workflow-key"
	}`
	got, err := extractWorkflowsWorkflow(workflowsWorkflowContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantKMSName := "//cloudkms.googleapis.com/projects/demo-project/locations/us-central1/keyRings/rk/cryptoKeys/workflow-key"
	assertRelationship(t, got.Relationships, relationshipTypeWorkflowsWorkflowEncryptedByKMSKey, wantKMSName, assetTypeKMSCryptoKey)
	if len(got.Relationships) != 1 {
		t.Fatalf("expected exactly 1 relationship, got %d: %#v", len(got.Relationships), got.Relationships)
	}
}

func TestExtractWorkflowsWorkflowMalformedDataErrors(t *testing.T) {
	_, err := extractWorkflowsWorkflow(workflowsWorkflowContext(`{not json`))
	if err == nil {
		t.Fatalf("expected an error for malformed resource data")
	}
}

func TestExtractWorkflowsWorkflowNeverDecodesSourceContentsBody(t *testing.T) {
	// A large sourceContents payload with an embedded secret-shaped token must
	// never leave the parser as anything but a boolean presence flag.
	const data = `{
		"state": "ACTIVE",
		"sourceContents": "main:\n  steps:\n  - callApi:\n      call: http.get\n      args:\n        url: https://internal.example.com/api\n        headers:\n          Authorization: Bearer super-secret-token\n"
	}`
	got, err := extractWorkflowsWorkflow(workflowsWorkflowContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Attributes["source_contents_present"] != true {
		t.Errorf("source_contents_present = %v, want true", got.Attributes["source_contents_present"])
	}
	blob, _ := json.Marshal(got)
	for _, banned := range []string{"super-secret-token", "internal.example.com", "Authorization"} {
		if containsString(string(blob), banned) {
			t.Fatalf("extraction leaked sourceContents token %q: %s", banned, blob)
		}
	}
}
