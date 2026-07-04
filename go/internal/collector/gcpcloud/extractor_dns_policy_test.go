// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"reflect"
	"testing"
)

const dnsPolicyFullName = "//dns.googleapis.com/projects/demo-project/policies/default-policy"

func dnsPolicyContext(data string) ExtractContext {
	return ExtractContext{
		FullResourceName: dnsPolicyFullName,
		AssetType:        dnsPolicyAssetType,
		ProjectID:        "demo-project",
		Data:             json.RawMessage(data),
	}
}

func TestDNSPolicyExtractorIsRegistered(t *testing.T) {
	if _, ok := lookupAssetExtractor(dnsPolicyAssetType); !ok {
		t.Fatalf("expected %q extractor to self-register via init()", dnsPolicyAssetType)
	}
}

func TestExtractDNSPolicyBasicFlags(t *testing.T) {
	const data = `{
		"name": "default-policy",
		"description": "org-wide policy",
		"enableInboundForwarding": true,
		"enableLogging": true
	}`

	got, err := extractDNSPolicy(dnsPolicyContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantAttrs := map[string]any{
		"enable_inbound_forwarding": true,
		"enable_logging":            true,
	}
	if !reflect.DeepEqual(got.Attributes, wantAttrs) {
		t.Fatalf("attributes mismatch:\n got %#v\nwant %#v", got.Attributes, wantAttrs)
	}
	if len(got.Relationships) != 0 {
		t.Fatalf("expected no edges with no bound networks, got %#v", got.Relationships)
	}
	if len(got.CorrelationAnchors) != 0 {
		t.Fatalf("expected no anchors with no bound networks, got %#v", got.CorrelationAnchors)
	}
}

func TestExtractDNSPolicyNetworksProduceEdgesAndAnchors(t *testing.T) {
	const data = `{
		"name": "vpc-policy",
		"enableInboundForwarding": false,
		"enableLogging": false,
		"networks": [
			{"networkUrl": "https://www.googleapis.com/compute/v1/projects/demo-project/global/networks/vpc-a"},
			{"networkUrl": "https://www.googleapis.com/compute/v1/projects/demo-project/global/networks/vpc-b"}
		]
	}`

	got, err := extractDNSPolicy(dnsPolicyContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.Attributes["network_count"] != 2 {
		t.Errorf("network_count = %v, want 2", got.Attributes["network_count"])
	}
	// An explicit false must be kept, not omitted, distinguishing "posture
	// observed disabled" from "field absent from a partial CAI page".
	if v, ok := got.Attributes["enable_inbound_forwarding"].(bool); !ok || v {
		t.Errorf("expected enable_inbound_forwarding=false (present, not omitted), got %#v", got.Attributes)
	}
	if v, ok := got.Attributes["enable_logging"].(bool); !ok || v {
		t.Errorf("expected enable_logging=false (present, not omitted), got %#v", got.Attributes)
	}

	const (
		vpcA = "//compute.googleapis.com/projects/demo-project/global/networks/vpc-a"
		vpcB = "//compute.googleapis.com/projects/demo-project/global/networks/vpc-b"
	)
	wantAnchors := []string{vpcA, vpcB}
	if !reflect.DeepEqual(got.CorrelationAnchors, wantAnchors) {
		t.Fatalf("anchors mismatch:\n got %#v\nwant %#v", got.CorrelationAnchors, wantAnchors)
	}
	if len(got.Relationships) != 2 {
		t.Fatalf("expected 2 network edges, got %d: %#v", len(got.Relationships), got.Relationships)
	}
	assertRelationship(t, got.Relationships, relationshipTypeDNSPolicyAppliesToNetwork, vpcA, assetTypeComputeNetwork)
	assertRelationship(t, got.Relationships, relationshipTypeDNSPolicyAppliesToNetwork, vpcB, assetTypeComputeNetwork)
	for _, rel := range got.Relationships {
		if rel.SourceFullResourceName != dnsPolicyFullName {
			t.Errorf("relationship source = %q, want policy full name", rel.SourceFullResourceName)
		}
		if rel.SourceAssetType != dnsPolicyAssetType {
			t.Errorf("relationship source asset type = %q, want %q", rel.SourceAssetType, dnsPolicyAssetType)
		}
	}
}

func TestExtractDNSPolicyAlternativeNameServerConfig(t *testing.T) {
	const data = `{
		"name": "forwarding-policy",
		"alternativeNameServerConfig": {
			"targetNameServers": [
				{"ipv4Address": "10.0.0.1", "forwardingPath": "private"},
				{"ipv4Address": "10.0.0.2", "forwardingPath": "default"},
				{"ipv6Address": "2001:db8::1", "forwardingPath": "default"}
			]
		}
	}`

	got, err := extractDNSPolicy(dnsPolicyContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.Attributes["alternative_name_server_count"] != 3 {
		t.Errorf("alternative_name_server_count = %v, want 3", got.Attributes["alternative_name_server_count"])
	}

	// Raw nameserver IPs must never leak into any attribute, anchor, or
	// relationship, regardless of the Go type they might arrive in. Marshal
	// the entire extraction struct and scan the resulting JSON so a future
	// regression that stores an IP as json.RawMessage, []byte, or any other
	// non-string type is still caught (mirrors the DNS Managed Zone
	// extractor's full-struct-marshal leak check for its own forwarding
	// target IPs).
	blob, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal extraction: %v", err)
	}
	for _, needle := range []string{"10.0.0.1", "10.0.0.2", "2001:db8::1"} {
		if containsString(string(blob), needle) {
			t.Fatalf("extraction leaked an alternative name server address %q: %s", needle, blob)
		}
	}
	if len(got.Relationships) != 0 {
		t.Fatalf("alternative name servers are not resolvable CAI endpoints, expected no edges, got %#v", got.Relationships)
	}
}

func TestExtractDNSPolicyAbsentBooleanFieldsOmitted(t *testing.T) {
	// A partial CAI page with enableInboundForwarding/enableLogging entirely
	// absent (the proto3-default-omitted wire shape) must never fabricate a
	// false posture; only a field the API actually reported becomes an
	// attribute, mirroring the Backend Service extractor's EnableCDN tri-state.
	const data = `{"name": "policy", "networks": []}`
	got, err := extractDNSPolicy(dnsPolicyContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := got.Attributes["enable_inbound_forwarding"]; ok {
		t.Errorf("expected enable_inbound_forwarding omitted when absent, got %#v", got.Attributes)
	}
	if _, ok := got.Attributes["enable_logging"]; ok {
		t.Errorf("expected enable_logging omitted when absent, got %#v", got.Attributes)
	}
}

func TestExtractDNSPolicyEmptyDataYieldsNothing(t *testing.T) {
	got, err := extractDNSPolicy(dnsPolicyContext(`{}`))
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

func TestExtractDNSPolicyMalformedDataErrors(t *testing.T) {
	if _, err := extractDNSPolicy(dnsPolicyContext(`{not json`)); err == nil {
		t.Fatalf("expected an error for malformed resource data")
	}
}

func TestExtractDNSPolicyNetworkURLResolutionSkipsUnresolvable(t *testing.T) {
	const data = `{
		"networks": [
			{"networkUrl": ""},
			{"networkUrl": "not-a-valid-network-reference"}
		]
	}`
	got, err := extractDNSPolicy(dnsPolicyContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Relationships) != 0 {
		t.Fatalf("expected no edges for unresolvable network references, got %#v", got.Relationships)
	}
	if len(got.CorrelationAnchors) != 0 {
		t.Fatalf("expected no anchors for unresolvable network references, got %#v", got.CorrelationAnchors)
	}
	if _, ok := got.Attributes["network_count"]; ok {
		t.Errorf("expected no network_count when no network resolved a URL, got %#v", got.Attributes)
	}
}

func TestExtractDNSPolicyDuplicateNetworksDeduped(t *testing.T) {
	// The same VPC network bound twice (or two networkUrl values that resolve
	// to the same full resource name) must not inflate network_count nor
	// produce duplicate anchors/edges, mirroring the dedup already applied to
	// CorrelationAnchors elsewhere in the extractor family.
	const data = `{
		"name": "vpc-policy",
		"networks": [
			{"networkUrl": "https://www.googleapis.com/compute/v1/projects/demo-project/global/networks/vpc-a"},
			{"networkUrl": "https://www.googleapis.com/compute/v1/projects/demo-project/global/networks/vpc-a"},
			{"networkUrl": "projects/demo-project/global/networks/vpc-a"}
		]
	}`

	got, err := extractDNSPolicy(dnsPolicyContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	const vpcA = "//compute.googleapis.com/projects/demo-project/global/networks/vpc-a"

	if got.Attributes["network_count"] != 1 {
		t.Errorf("network_count = %v, want 1 (deduped)", got.Attributes["network_count"])
	}
	if !reflect.DeepEqual(got.CorrelationAnchors, []string{vpcA}) {
		t.Fatalf("anchors not deduped: got %#v", got.CorrelationAnchors)
	}
	if len(got.Relationships) != 1 {
		t.Fatalf("expected exactly 1 deduped edge, got %d: %#v", len(got.Relationships), got.Relationships)
	}
}

func TestExtractDNSPolicyTargetNameServersNeverRetainRawJSON(t *testing.T) {
	// TargetNameServers must decode only redaction-safe metadata (forwarding
	// path presence/value); no field on the decode target may hold the raw
	// per-element JSON (which carries ipv4Address/ipv6Address), even
	// transiently, since a future debug/error path could serialize the
	// decode target itself rather than the returned AttributeExtraction.
	const data = `{
		"alternativeNameServerConfig": {
			"targetNameServers": [
				{"ipv4Address": "10.0.0.1", "forwardingPath": "private"},
				{"ipv6Address": "2001:db8::1", "forwardingPath": "default"}
			]
		}
	}`

	var decoded dnsPolicyData
	if err := json.Unmarshal([]byte(data), &decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}

	blob, err := json.Marshal(decoded)
	if err != nil {
		t.Fatalf("marshal decode target: %v", err)
	}
	for _, needle := range []string{"10.0.0.1", "2001:db8::1"} {
		if containsString(string(blob), needle) {
			t.Fatalf("decode target retains raw name-server JSON %q: %s", needle, blob)
		}
	}
}

func TestExtractDNSPolicyNoDescriptionAttribute(t *testing.T) {
	// description is free-form operator text, not a bounded control-plane
	// field usable for Terraform import/drift, edges, correlation, or
	// monitoring, so it must never be persisted as a typed attribute.
	const data = `{"name": "policy", "description": "internal notes about this policy"}`
	got, err := extractDNSPolicy(dnsPolicyContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := got.Attributes["description"]; ok {
		t.Fatalf("description must never be persisted raw: %#v", got.Attributes)
	}
	blob, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal extraction: %v", err)
	}
	if containsString(string(blob), "internal notes") {
		t.Fatalf("extraction leaked the policy description: %s", blob)
	}
}
