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

const orgPolicyOrgFixtureFullName = "//orgpolicy.googleapis.com/organizations/987654321/policies/compute.requireOsLogin"

// TestOrgPolicyOfflineFixtureEndToEnd exercises the offline assets.list fixture
// for Organization Policy through parse -> normalize -> attribute extraction ->
// generation -> envelope, proving the redaction-safe typed-depth attributes and
// the applies-to-resource edge reach durable facts without any live GCP call,
// and that no rule value, condition expression, or raw spec etag ever lands on a
// fact. This is the wiring proof the direct-call unit test cannot give: it
// confirms parse.go dispatches to the registered extractor and its output lands
// on real gcp_cloud_resource and gcp_cloud_relationship facts.
func TestOrgPolicyOfflineFixtureEndToEnd(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "assets_list_org_policy.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	page, err := ParseAssetsListPage(raw)
	if err != nil {
		t.Fatalf("parse fixture page: %v", err)
	}
	if len(page.Resources) != 2 {
		t.Fatalf("expected 2 resources, got %d", len(page.Resources))
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
	appliesToEdges := 0
	var orgAttrs map[string]any
	edgeTargets := map[string]string{}
	for _, env := range envelopes {
		switch env.FactKind {
		case facts.GCPCloudResourceFactKind:
			resourceCount++
			if env.Payload["full_resource_name"] == orgPolicyOrgFixtureFullName {
				orgAttrs, _ = env.Payload["attributes"].(map[string]any)
			}
		case facts.GCPCloudRelationshipFactKind:
			if stringOrEmpty(env.Payload["relationship_type"]) == relationshipTypeOrgPolicyAppliesToResource {
				appliesToEdges++
				edgeTargets[stringOrEmpty(env.Payload["source_full_resource_name"])] = stringOrEmpty(env.Payload["target_full_resource_name"])
			}
		}
	}

	if resourceCount != 2 {
		t.Errorf("gcp_cloud_resource facts = %d, want 2", resourceCount)
	}
	if orgAttrs == nil {
		t.Fatalf("org-level policy carried no attributes")
	}
	if orgAttrs["constraint_name"] != "compute.requireOsLogin" {
		t.Errorf("org policy constraint_name = %v, want compute.requireOsLogin", orgAttrs["constraint_name"])
	}
	if orgAttrs["enforce_count"] != float64(1) && orgAttrs["enforce_count"] != 1 {
		t.Errorf("org policy enforce_count = %v (%T), want 1", orgAttrs["enforce_count"], orgAttrs["enforce_count"])
	}

	// Both the org-level and the project-level policy resolve their applies-to
	// edge; the org edge targets the Organization node, the project edge the
	// Project node.
	if appliesToEdges != 2 {
		t.Errorf("org_policy_applies_to_resource edges = %d, want 2", appliesToEdges)
	}
	if got := edgeTargets[orgPolicyOrgFixtureFullName]; got != "//cloudresourcemanager.googleapis.com/organizations/987654321" {
		t.Errorf("org edge target = %q, want organization node", got)
	}
	const projFullName = "//orgpolicy.googleapis.com/projects/123456789/policies/gcp.resourceLocations"
	if got := edgeTargets[projFullName]; got != "//cloudresourcemanager.googleapis.com/projects/123456789" {
		t.Errorf("project edge target = %q, want project node", got)
	}

	// No rule value, condition expression text, or raw spec etag may reach any
	// fact. The fixture deliberately embeds allow/deny values, a CEL expression,
	// and two etags; none may appear in the serialized envelope set.
	blob, err := json.Marshal(envelopes)
	if err != nil {
		t.Fatalf("marshal envelopes: %v", err)
	}
	for _, token := range []string{
		"in:us-locations", "in:eu-locations", "in:asia-locations",
		"resource.matchTag", "BwXhwFakeEtag=", "BwXhwOtherEtag=",
	} {
		if containsString(string(blob), token) {
			t.Fatalf("envelope set leaked forbidden token %q", token)
		}
	}
}
