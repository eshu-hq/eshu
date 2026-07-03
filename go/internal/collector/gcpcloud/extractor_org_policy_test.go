// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"testing"
)

const orgPolicyOrgFullName = "//orgpolicy.googleapis.com/organizations/123456789/policies/compute.requireOsLogin"

func orgPolicyContext(fullName, data string) ExtractContext {
	return ExtractContext{
		FullResourceName: fullName,
		AssetType:        orgPolicyAssetType,
		ProjectID:        "",
		Data:             json.RawMessage(data),
	}
}

func TestOrgPolicyExtractorIsRegistered(t *testing.T) {
	if _, ok := lookupAssetExtractor(orgPolicyAssetType); !ok {
		t.Fatalf("expected %q extractor to self-register via init()", orgPolicyAssetType)
	}
}

func TestExtractOrgPolicyOrganizationTarget(t *testing.T) {
	const data = `{
		"name": "organizations/123456789/policies/compute.requireOsLogin",
		"spec": {
			"etag": "BwXhwFakeEtag=",
			"updateTime": "2026-01-15T10:00:00Z",
			"inheritFromParent": false,
			"reset": false,
			"rules": [
				{"enforce": true}
			]
		}
	}`

	got, err := extractOrgPolicy(orgPolicyContext(orgPolicyOrgFullName, data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.Attributes["constraint_name"] != "compute.requireOsLogin" {
		t.Errorf("constraint_name = %v, want compute.requireOsLogin", got.Attributes["constraint_name"])
	}
	if got.Attributes["rule_count"] != 1 {
		t.Errorf("rule_count = %v, want 1", got.Attributes["rule_count"])
	}
	if got.Attributes["enforce_count"] != 1 {
		t.Errorf("enforce_count = %v, want 1", got.Attributes["enforce_count"])
	}
	if got.Attributes["inherit_from_parent"] != false {
		t.Errorf("inherit_from_parent = %v, want false", got.Attributes["inherit_from_parent"])
	}
	if got.Attributes["reset"] != false {
		t.Errorf("reset = %v, want false", got.Attributes["reset"])
	}
	if v, ok := got.Attributes["etag_fingerprint"].(string); !ok || v == "" {
		t.Errorf("expected non-empty etag_fingerprint, got %#v", got.Attributes["etag_fingerprint"])
	}
	if got.Attributes["update_time"] != "2026-01-15T10:00:00Z" {
		t.Errorf("update_time = %v, want 2026-01-15T10:00:00Z", got.Attributes["update_time"])
	}

	if len(got.Relationships) != 1 {
		t.Fatalf("expected 1 relationship, got %#v", got.Relationships)
	}
	rel := got.Relationships[0]
	wantTarget := "//cloudresourcemanager.googleapis.com/organizations/123456789"
	if rel.TargetFullResourceName != wantTarget {
		t.Errorf("target = %q, want %q", rel.TargetFullResourceName, wantTarget)
	}
	if rel.TargetAssetType != assetTypeCloudResourceManagerOrganization {
		t.Errorf("target asset type = %q, want %q", rel.TargetAssetType, assetTypeCloudResourceManagerOrganization)
	}
	if rel.RelationshipType != relationshipTypeOrgPolicyAppliesToResource {
		t.Errorf("relationship type = %q, want %q", rel.RelationshipType, relationshipTypeOrgPolicyAppliesToResource)
	}
	if rel.SupportState != RelationshipSupportSupported {
		t.Errorf("support state = %q, want supported", rel.SupportState)
	}

	if len(got.CorrelationAnchors) != 1 || got.CorrelationAnchors[0] != wantTarget {
		t.Errorf("anchors = %#v, want [%q]", got.CorrelationAnchors, wantTarget)
	}
}

func TestExtractOrgPolicyFolderTarget(t *testing.T) {
	const fullName = "//orgpolicy.googleapis.com/folders/998877/policies/iam.disableServiceAccountKeyCreation"
	const data = `{"spec": {"rules": [{"enforce": true}]}}`

	got, err := extractOrgPolicy(orgPolicyContext(fullName, data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Attributes["constraint_name"] != "iam.disableServiceAccountKeyCreation" {
		t.Errorf("constraint_name = %v", got.Attributes["constraint_name"])
	}
	if len(got.Relationships) != 1 {
		t.Fatalf("expected 1 relationship, got %#v", got.Relationships)
	}
	rel := got.Relationships[0]
	wantTarget := "//cloudresourcemanager.googleapis.com/folders/998877"
	if rel.TargetFullResourceName != wantTarget {
		t.Errorf("target = %q, want %q", rel.TargetFullResourceName, wantTarget)
	}
	if rel.TargetAssetType != assetTypeCloudResourceManagerFolder {
		t.Errorf("target asset type = %q, want %q", rel.TargetAssetType, assetTypeCloudResourceManagerFolder)
	}
}

func TestExtractOrgPolicyProjectTarget(t *testing.T) {
	const fullName = "//orgpolicy.googleapis.com/projects/445566/policies/compute.vmExternalIpAccess"
	const data = `{"spec": {"rules": [{"values": {"deniedValues": ["a", "b"]}}]}}`

	got, err := extractOrgPolicy(orgPolicyContext(fullName, data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Relationships) != 1 {
		t.Fatalf("expected 1 relationship, got %#v", got.Relationships)
	}
	rel := got.Relationships[0]
	wantTarget := "//cloudresourcemanager.googleapis.com/projects/445566"
	if rel.TargetFullResourceName != wantTarget {
		t.Errorf("target = %q, want %q", rel.TargetFullResourceName, wantTarget)
	}
	if rel.TargetAssetType != assetTypeCloudResourceManagerProject {
		t.Errorf("target asset type = %q, want %q", rel.TargetAssetType, assetTypeCloudResourceManagerProject)
	}
}

func TestExtractOrgPolicyRuleValuesSummary(t *testing.T) {
	const data = `{
		"spec": {
			"rules": [
				{"values": {"allowedValues": ["a", "b", "c"]}},
				{"values": {"deniedValues": ["x"]}},
				{"allowAll": true},
				{"denyAll": true},
				{"enforce": false, "condition": {"expression": "resource.name == 'x'"}}
			]
		}
	}`
	got, err := extractOrgPolicy(orgPolicyContext(orgPolicyOrgFullName, data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Attributes["rule_count"] != 5 {
		t.Errorf("rule_count = %v, want 5", got.Attributes["rule_count"])
	}
	if got.Attributes["allow_values_rule_count"] != 1 {
		t.Errorf("allow_values_rule_count = %v, want 1", got.Attributes["allow_values_rule_count"])
	}
	if got.Attributes["deny_values_rule_count"] != 1 {
		t.Errorf("deny_values_rule_count = %v, want 1", got.Attributes["deny_values_rule_count"])
	}
	if got.Attributes["allow_all_rule_count"] != 1 {
		t.Errorf("allow_all_rule_count = %v, want 1", got.Attributes["allow_all_rule_count"])
	}
	if got.Attributes["deny_all_rule_count"] != 1 {
		t.Errorf("deny_all_rule_count = %v, want 1", got.Attributes["deny_all_rule_count"])
	}
	if got.Attributes["condition_rule_count"] != 1 {
		t.Errorf("condition_rule_count = %v, want 1", got.Attributes["condition_rule_count"])
	}
	if _, ok := got.Attributes["enforce_count"]; ok {
		t.Errorf("no rule enforces true; enforce_count must be omitted: %#v", got.Attributes)
	}
	// The raw allowed/denied values themselves must never be persisted.
	blob, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal extraction: %v", err)
	}
	for _, leaked := range []string{"\"a\"", "\"b\"", "\"c\"", "\"x\""} {
		if containsString(string(blob), leaked) {
			t.Fatalf("extraction leaked raw rule value %q: %s", leaked, blob)
		}
	}
}

func TestExtractOrgPolicyDryRunSpec(t *testing.T) {
	const data = `{
		"dryRunSpec": {
			"rules": [{"enforce": true}]
		}
	}`
	got, err := extractOrgPolicy(orgPolicyContext(orgPolicyOrgFullName, data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Attributes["has_dry_run_spec"] != true {
		t.Errorf("has_dry_run_spec = %v, want true", got.Attributes["has_dry_run_spec"])
	}
	if got.Attributes["dry_run_rule_count"] != 1 {
		t.Errorf("dry_run_rule_count = %v, want 1", got.Attributes["dry_run_rule_count"])
	}
}

func TestExtractOrgPolicyNeverPersistsEtagRaw(t *testing.T) {
	const rawEtag = "BwXhwFakeEtag="
	const data = `{"spec": {"etag": "BwXhwFakeEtag=", "rules": [{"enforce": true}]}}`
	got, err := extractOrgPolicy(orgPolicyContext(orgPolicyOrgFullName, data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	blob, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal extraction: %v", err)
	}
	if containsString(string(blob), rawEtag) {
		t.Fatalf("extraction leaked raw etag %q: %s", rawEtag, blob)
	}
}

func TestExtractOrgPolicyEmptyDataYieldsNothing(t *testing.T) {
	got, err := extractOrgPolicy(orgPolicyContext(orgPolicyOrgFullName, `{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := got.Attributes["rule_count"]; ok {
		t.Errorf("empty spec must omit rule_count: %#v", got.Attributes)
	}
	// Even with no spec, the target-resource edge still derives from the
	// policy's own full resource name.
	if len(got.Relationships) != 1 {
		t.Errorf("expected 1 relationship derived from full resource name, got %#v", got.Relationships)
	}
}

func TestExtractOrgPolicyUnparsableFullNameOmitsEdge(t *testing.T) {
	got, err := extractOrgPolicy(orgPolicyContext("//orgpolicy.googleapis.com/malformed", `{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Relationships) != 0 {
		t.Errorf("malformed full resource name must yield no edge, got %#v", got.Relationships)
	}
	if len(got.CorrelationAnchors) != 0 {
		t.Errorf("malformed full resource name must yield no anchor, got %#v", got.CorrelationAnchors)
	}
	if _, ok := got.Attributes["constraint_name"]; ok {
		t.Errorf("malformed full resource name must omit constraint_name: %#v", got.Attributes)
	}
}

func TestExtractOrgPolicyMalformedDataErrors(t *testing.T) {
	if _, err := extractOrgPolicy(orgPolicyContext(orgPolicyOrgFullName, `{not json`)); err == nil {
		t.Fatalf("expected an error for malformed resource data")
	}
}

// TestExtractOrgPolicyUntrustedFullNameMintsNoEdge proves the correlation-truth
// guard at the extractor boundary: ctx.FullResourceName is the raw, untrusted
// CAI asset name, so a relative or wrong-service name that happens to carry an
// `/organizations/<id>/policies/<x>` shape must fabricate no edge, anchor, or
// constraint_name — a fabricated `org_policy_applies_to_resource` edge would
// point at a real-looking hierarchy node the policy is not actually bound to.
func TestExtractOrgPolicyUntrustedFullNameMintsNoEdge(t *testing.T) {
	const spec = `{"spec": {"rules": [{"enforce": true}]}}`
	cases := []string{
		"organizations/123/policies/compute.requireOsLogin",             // relative, no service prefix
		"//compute.googleapis.com/organizations/123/policies/compute.x", // wrong service
		"/organizations/123/policies/compute.x",                         // single-slash relative
	}
	for _, fullName := range cases {
		t.Run(fullName, func(t *testing.T) {
			got, err := extractOrgPolicy(orgPolicyContext(fullName, spec))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got.Relationships) != 0 {
				t.Errorf("untrusted full name %q minted an edge: %#v", fullName, got.Relationships)
			}
			if len(got.CorrelationAnchors) != 0 {
				t.Errorf("untrusted full name %q minted an anchor: %#v", fullName, got.CorrelationAnchors)
			}
			if _, ok := got.Attributes["constraint_name"]; ok {
				t.Errorf("untrusted full name %q surfaced a constraint_name: %#v", fullName, got.Attributes)
			}
			// The spec rule-shape attributes still land — only the
			// full-name-derived fields fail closed.
			if got.Attributes["enforce_count"] != 1 {
				t.Errorf("spec attributes should still extract; enforce_count = %v", got.Attributes["enforce_count"])
			}
		})
	}
}

func TestOrgPolicyTargetFromFullResourceName(t *testing.T) {
	cases := []struct {
		name           string
		fullName       string
		wantConstraint string
		wantTarget     string
		wantAssetType  string
		wantOK         bool
	}{
		{
			name:           "organization",
			fullName:       "//orgpolicy.googleapis.com/organizations/123/policies/compute.foo",
			wantConstraint: "compute.foo",
			wantTarget:     "//cloudresourcemanager.googleapis.com/organizations/123",
			wantAssetType:  assetTypeCloudResourceManagerOrganization,
			wantOK:         true,
		},
		{
			name:           "folder",
			fullName:       "//orgpolicy.googleapis.com/folders/456/policies/iam.bar",
			wantConstraint: "iam.bar",
			wantTarget:     "//cloudresourcemanager.googleapis.com/folders/456",
			wantAssetType:  assetTypeCloudResourceManagerFolder,
			wantOK:         true,
		},
		{
			name:           "project",
			fullName:       "//orgpolicy.googleapis.com/projects/789/policies/storage.baz",
			wantConstraint: "storage.baz",
			wantTarget:     "//cloudresourcemanager.googleapis.com/projects/789",
			wantAssetType:  assetTypeCloudResourceManagerProject,
			wantOK:         true,
		},
		{
			name:     "no policies segment",
			fullName: "//orgpolicy.googleapis.com/organizations/123",
			wantOK:   false,
		},
		{
			name:     "unrecognized parent kind",
			fullName: "//orgpolicy.googleapis.com/billingAccounts/123/policies/foo",
			wantOK:   false,
		},
		{
			name:     "blank",
			fullName: "",
			wantOK:   false,
		},
		{
			// Fail closed: a relative name missing the service prefix but
			// carrying an org/policies shape must NOT mint a fabricated edge.
			name:     "missing service prefix (relative name)",
			fullName: "organizations/123/policies/compute.foo",
			wantOK:   false,
		},
		{
			// Fail closed: a different-service prefix with a /policies/ suffix
			// must not resolve as an org policy.
			name:     "wrong service prefix",
			fullName: "//compute.googleapis.com/organizations/123/policies/compute.foo",
			wantOK:   false,
		},
		{
			// Fail closed: a leading-slash-only relative name must not slip
			// through the HasPrefix guard.
			name:     "single-slash relative name",
			fullName: "/organizations/123/policies/compute.foo",
			wantOK:   false,
		},
		{
			// Fail closed: extra segments between the id and /policies/ mean the
			// parent path is not exactly <kind>/<id>, so no hierarchy node is
			// resolvable.
			name:     "extra parent path segments",
			fullName: "//orgpolicy.googleapis.com/organizations/123/subthing/9/policies/compute.foo",
			wantOK:   false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			constraint, target, assetType, ok := orgPolicyTargetFromFullResourceName(tc.fullName)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if !ok {
				return
			}
			if constraint != tc.wantConstraint {
				t.Errorf("constraint = %q, want %q", constraint, tc.wantConstraint)
			}
			if target != tc.wantTarget {
				t.Errorf("target = %q, want %q", target, tc.wantTarget)
			}
			if assetType != tc.wantAssetType {
				t.Errorf("assetType = %q, want %q", assetType, tc.wantAssetType)
			}
		})
	}
}
