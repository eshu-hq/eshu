// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"reflect"
	"testing"
)

const iamRoleFullName = "//iam.googleapis.com/projects/demo-project/roles/customDeployer"

func iamRoleContext(data string) ExtractContext {
	return ExtractContext{
		FullResourceName: iamRoleFullName,
		AssetType:        iamRoleAssetType,
		ProjectID:        "demo-project",
		Data:             json.RawMessage(data),
	}
}

func TestIAMRoleExtractorIsRegistered(t *testing.T) {
	if _, ok := lookupAssetExtractor(iamRoleAssetType); !ok {
		t.Fatalf("expected %q extractor to self-register via init()", iamRoleAssetType)
	}
}

func TestExtractIAMRoleFullResource(t *testing.T) {
	const data = `{
		"name": "projects/demo-project/roles/customDeployer",
		"title": "Custom Deployer",
		"description": "Deploys workloads",
		"stage": "GA",
		"includedPermissions": [
			"compute.instances.get",
			"compute.instances.list",
			"iam.serviceAccounts.actAs",
			"resourcemanager.projects.setIamPolicy"
		],
		"etag": "BwXhwFakeEtag=",
		"deleted": false
	}`

	got, err := extractIAMRole(iamRoleContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantAttrs := map[string]any{
		"title":                       "Custom Deployer",
		"stage":                       "GA",
		"included_permission_count":   4,
		"sensitive_permission_count":  2,
		"grants_privilege_escalation": true,
		"deleted":                     false,
		"etag_fingerprint":            iamRoleEtagFingerprint("BwXhwFakeEtag="),
	}
	if !reflect.DeepEqual(got.Attributes, wantAttrs) {
		t.Fatalf("attributes mismatch:\n got %#v\nwant %#v", got.Attributes, wantAttrs)
	}
	if len(got.Relationships) != 0 {
		t.Errorf("IAM Role derives no outbound edges (bindings are inbound), got %#v", got.Relationships)
	}
	if len(got.CorrelationAnchors) != 0 {
		t.Errorf("IAM Role derives no outbound anchors, got %#v", got.CorrelationAnchors)
	}
}

func TestExtractIAMRoleDisabledStageAndDeleted(t *testing.T) {
	const data = `{
		"name": "projects/demo-project/roles/legacy",
		"title": "Legacy",
		"stage": "DISABLED",
		"includedPermissions": ["storage.objects.get"],
		"deleted": true
	}`
	got, err := extractIAMRole(iamRoleContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Attributes["stage"] != "DISABLED" {
		t.Errorf("stage = %v, want DISABLED", got.Attributes["stage"])
	}
	if got.Attributes["deleted"] != true {
		t.Errorf("deleted = %v, want true", got.Attributes["deleted"])
	}
	if got.Attributes["included_permission_count"] != 1 {
		t.Errorf("included_permission_count = %v, want 1", got.Attributes["included_permission_count"])
	}
	if _, ok := got.Attributes["sensitive_permission_count"]; ok {
		t.Errorf("no sensitive permission present; count must be omitted: %#v", got.Attributes)
	}
	if _, ok := got.Attributes["grants_privilege_escalation"]; ok {
		t.Errorf("no sensitive permission present; escalation flag must be omitted: %#v", got.Attributes)
	}
}

func TestExtractIAMRoleEmptyPermissionsOmitsCount(t *testing.T) {
	// A role with no reported permissions must not fabricate a "0 permissions"
	// posture: the count is omitted, consistent with the other *_count attributes.
	const data = `{"title": "Empty", "stage": "GA", "includedPermissions": []}`
	got, err := extractIAMRole(iamRoleContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := got.Attributes["included_permission_count"]; ok {
		t.Errorf("empty permissions must omit included_permission_count: %#v", got.Attributes)
	}
}

func TestExtractIAMRoleAbsentDeletedOmitted(t *testing.T) {
	// deleted is a pointer: an absent field must be omitted, distinct from a
	// present false (an active role, useful posture).
	const data = `{"title": "Active", "stage": "GA"}`
	got, err := extractIAMRole(iamRoleContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := got.Attributes["deleted"]; ok {
		t.Errorf("absent deleted must be omitted: %#v", got.Attributes)
	}
}

func TestExtractIAMRoleNeverPersistsEtagRaw(t *testing.T) {
	const rawEtag = "BwXhwFakeEtag="
	const data = `{"title": "Role", "stage": "GA", "etag": "BwXhwFakeEtag="}`
	got, err := extractIAMRole(iamRoleContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	blob, _ := json.Marshal(got)
	if containsString(string(blob), rawEtag) {
		t.Fatalf("extraction leaked raw etag %q: %s", rawEtag, blob)
	}
	if got.Attributes["etag_fingerprint"] == "" {
		t.Errorf("expected an etag fingerprint, got empty")
	}
}

func TestExtractIAMRoleEmptyDataYieldsNothing(t *testing.T) {
	got, err := extractIAMRole(iamRoleContext(`{}`))
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

func TestExtractIAMRoleMalformedDataErrors(t *testing.T) {
	if _, err := extractIAMRole(iamRoleContext(`{not json`)); err == nil {
		t.Fatalf("expected an error for malformed resource data")
	}
}

func TestIsSensitiveIAMPermission(t *testing.T) {
	cases := []struct {
		perm string
		want bool
	}{
		{"resourcemanager.projects.setIamPolicy", true},
		{"iam.serviceAccounts.actAs", true},
		{"iam.serviceAccounts.getAccessToken", true},
		{"iam.serviceAccountKeys.create", true},
		{"iam.roles.update", true},
		{"compute.instances.get", false},
		{"storage.objects.list", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := isSensitiveIAMPermission(tc.perm); got != tc.want {
			t.Errorf("isSensitiveIAMPermission(%q) = %v, want %v", tc.perm, got, tc.want)
		}
	}
}
