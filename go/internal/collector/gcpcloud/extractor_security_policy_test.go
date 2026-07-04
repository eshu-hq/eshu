// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"reflect"
	"testing"
)

const securityPolicyFullName = "//compute.googleapis.com/projects/demo-project/global/securityPolicies/armor-policy"

func securityPolicyContext(data string) ExtractContext {
	return ExtractContext{
		FullResourceName: securityPolicyFullName,
		AssetType:        assetTypeComputeSecurityPolicy,
		ProjectID:        "demo-project",
		Data:             json.RawMessage(data),
	}
}

func TestSecurityPolicyExtractorIsRegistered(t *testing.T) {
	if _, ok := lookupAssetExtractor(assetTypeComputeSecurityPolicy); !ok {
		t.Fatalf("expected %q extractor to self-register via init()", assetTypeComputeSecurityPolicy)
	}
}

func TestExtractSecurityPolicyFullResource(t *testing.T) {
	const data = `{
		"name": "armor-policy",
		"description": "Blocks known bad actors",
		"type": "CLOUD_ARMOR",
		"creationTimestamp": "2024-06-01T00:00:00.000-07:00",
		"adaptiveProtectionConfig": {
			"layer7DdosDefenseConfig": {
				"enable": true,
				"ruleVisibility": "STANDARD"
			}
		},
		"rules": [
			{
				"priority": 1000,
				"action": "deny(403)",
				"description": "Block bad IPs",
				"match": {"versionedExpr": "SRC_IPS_V1"},
				"preview": false
			},
			{
				"priority": 2147483647,
				"action": "allow",
				"description": "Default rule",
				"match": {"config": {"srcIpRanges": ["*"]}},
				"preview": false
			}
		]
	}`

	got, err := extractSecurityPolicy(securityPolicyContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantAttrs := map[string]any{
		"type":                        "CLOUD_ARMOR",
		"creation_time":               "2024-06-01T07:00:00Z",
		"adaptive_protection_enabled": true,
		"rule_count":                  2,
		"rules": []map[string]any{
			{"priority": int32(1000), "action": "deny(403)"},
			{"priority": int32(2147483647), "action": "allow"},
		},
	}
	if !reflect.DeepEqual(got.Attributes, wantAttrs) {
		t.Fatalf("attributes mismatch:\n got %#v\nwant %#v", got.Attributes, wantAttrs)
	}
	if len(got.Relationships) != 0 {
		t.Errorf("SecurityPolicy derives no outbound edges (backend services own the inbound edge), got %#v", got.Relationships)
	}
	if len(got.CorrelationAnchors) != 0 {
		t.Errorf("SecurityPolicy derives no outbound anchors, got %#v", got.CorrelationAnchors)
	}
}

func TestExtractSecurityPolicyRegionalPolicyCapturesRegion(t *testing.T) {
	const data = `{
		"name": "regional-armor-policy",
		"type": "CLOUD_ARMOR",
		"region": "https://www.googleapis.com/compute/v1/projects/demo-project/regions/us-central1"
	}`

	got, err := extractSecurityPolicy(securityPolicyContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Attributes["region"] != "us-central1" {
		t.Errorf("region = %v, want us-central1", got.Attributes["region"])
	}
}

func TestExtractSecurityPolicyGlobalPolicyOmitsRegion(t *testing.T) {
	const data = `{"name": "global-armor-policy", "type": "CLOUD_ARMOR"}`
	got, err := extractSecurityPolicy(securityPolicyContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := got.Attributes["region"]; ok {
		t.Errorf("global policy must omit region: %#v", got.Attributes)
	}
}

func TestExtractSecurityPolicyEdgeAndNetworkTypes(t *testing.T) {
	cases := []struct {
		name       string
		policyType string
	}{
		{"edge policy", "CLOUD_ARMOR_EDGE"},
		{"network policy", "CLOUD_ARMOR_NETWORK"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := `{"name": "p", "type": "` + tc.policyType + `"}`
			got, err := extractSecurityPolicy(securityPolicyContext(data))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Attributes["type"] != tc.policyType {
				t.Errorf("type = %v, want %v", got.Attributes["type"], tc.policyType)
			}
		})
	}
}

func TestExtractSecurityPolicyPriorityZeroRulePreservesPriority(t *testing.T) {
	// Priority 0 is the highest-priority value in the Compute SecurityPolicyRule
	// schema ("a positive value between 0 and 2147483647"), not an absent-field
	// sentinel. A rule at priority 0 must keep its priority in the summary.
	const data = `{
		"name": "p",
		"type": "CLOUD_ARMOR",
		"rules": [{"priority": 0, "action": "deny(403)"}]
	}`
	got, err := extractSecurityPolicy(securityPolicyContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	rules, ok := got.Attributes["rules"].([]map[string]any)
	if !ok || len(rules) != 1 {
		t.Fatalf("expected one rule summary, got %#v", got.Attributes["rules"])
	}
	priority, ok := rules[0]["priority"]
	if !ok {
		t.Fatalf("priority-0 rule must not omit its priority: %#v", rules[0])
	}
	if priority != int32(0) {
		t.Errorf("priority = %v, want 0", priority)
	}
}

func TestExtractSecurityPolicyNoRulesOmitsRuleFields(t *testing.T) {
	const data = `{"name": "p", "type": "CLOUD_ARMOR", "rules": []}`
	got, err := extractSecurityPolicy(securityPolicyContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := got.Attributes["rule_count"]; ok {
		t.Errorf("empty rules must omit rule_count: %#v", got.Attributes)
	}
	if _, ok := got.Attributes["rules"]; ok {
		t.Errorf("empty rules must omit rules summary: %#v", got.Attributes)
	}
}

func TestExtractSecurityPolicyAdaptiveProtectionDisabledOmitted(t *testing.T) {
	const data = `{
		"name": "p",
		"type": "CLOUD_ARMOR",
		"adaptiveProtectionConfig": {
			"layer7DdosDefenseConfig": {"enable": false}
		}
	}`
	got, err := extractSecurityPolicy(securityPolicyContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Attributes["adaptive_protection_enabled"] != false {
		t.Errorf("adaptive_protection_enabled = %v, want explicit false", got.Attributes["adaptive_protection_enabled"])
	}
}

func TestExtractSecurityPolicyAdaptiveProtectionAbsentOmitted(t *testing.T) {
	const data = `{"name": "p", "type": "CLOUD_ARMOR"}`
	got, err := extractSecurityPolicy(securityPolicyContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := got.Attributes["adaptive_protection_enabled"]; ok {
		t.Errorf("absent adaptiveProtectionConfig must omit adaptive_protection_enabled: %#v", got.Attributes)
	}
}

func TestExtractSecurityPolicyNeverPersistsRuleMatchOrDescription(t *testing.T) {
	const rawMatchMarker = "SRC_IPS_V1"
	const rawDescriptionMarker = "super-secret-rule-description"
	data := `{
		"name": "p",
		"type": "CLOUD_ARMOR",
		"description": "` + rawDescriptionMarker + `",
		"rules": [
			{
				"priority": 1000,
				"action": "deny(403)",
				"description": "` + rawDescriptionMarker + `",
				"match": {"versionedExpr": "` + rawMatchMarker + `", "config": {"srcIpRanges": ["203.0.113.0/24"]}}
			}
		]
	}`
	got, err := extractSecurityPolicy(securityPolicyContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	blob, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal extraction: %v", err)
	}
	if containsString(string(blob), rawMatchMarker) {
		t.Fatalf("extraction leaked raw rule match expression: %s", blob)
	}
	if containsString(string(blob), rawDescriptionMarker) {
		t.Fatalf("extraction leaked raw description: %s", blob)
	}
	if containsString(string(blob), "203.0.113.0/24") {
		t.Fatalf("extraction leaked raw IP range: %s", blob)
	}
}

func TestExtractSecurityPolicyEmptyDataYieldsNothing(t *testing.T) {
	got, err := extractSecurityPolicy(securityPolicyContext(`{}`))
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

func TestExtractSecurityPolicyMalformedDataErrors(t *testing.T) {
	if _, err := extractSecurityPolicy(securityPolicyContext(`{not json`)); err == nil {
		t.Fatalf("expected an error for malformed resource data")
	}
}
