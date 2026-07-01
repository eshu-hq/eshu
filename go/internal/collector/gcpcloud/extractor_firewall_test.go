// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/secretsiam"
)

const firewallFullName = "//compute.googleapis.com/projects/demo-project/global/firewalls/allow-web"

func firewallContext(data string) ExtractContext {
	return ExtractContext{
		FullResourceName: firewallFullName,
		AssetType:        assetTypeComputeFirewall,
		ProjectID:        "demo-project",
		Data:             json.RawMessage(data),
	}
}

func TestFirewallExtractorIsRegistered(t *testing.T) {
	if _, ok := lookupAssetExtractor(assetTypeComputeFirewall); !ok {
		t.Fatalf("expected %q extractor to self-register via init()", assetTypeComputeFirewall)
	}
}

func TestExtractFirewallFullResource(t *testing.T) {
	const data = `{
		"name": "allow-web",
		"network": "https://www.googleapis.com/compute/v1/projects/demo-project/global/networks/prod-vpc",
		"direction": "INGRESS",
		"priority": 1000,
		"disabled": false,
		"sourceRanges": ["0.0.0.0/0"],
		"targetTags": ["web", "web"],
		"targetServiceAccounts": ["runtime@demo-project.iam.gserviceaccount.com"],
		"allowed": [
			{"IPProtocol": "tcp", "ports": ["80", "443"]},
			{"IPProtocol": "udp"}
		],
		"logConfig": {"enable": true}
	}`

	got, err := extractFirewall(firewallContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantAttrs := map[string]any{
		"direction":                    "INGRESS",
		"priority":                     int64(1000),
		"disabled":                     false,
		"log_enabled":                  true,
		"allowed_protocols":            []string{"tcp", "udp"},
		"allowed_ports":                []string{"80", "443"},
		"source_range_count":           1,
		"opens_to_public":              true,
		"target_tags":                  []string{"web"},
		"target_service_account_count": 1,
	}
	if !reflect.DeepEqual(got.Attributes, wantAttrs) {
		t.Fatalf("attributes mismatch:\n got %#v\nwant %#v", got.Attributes, wantAttrs)
	}

	const network = "//compute.googleapis.com/projects/demo-project/global/networks/prod-vpc"
	saDigest := secretsiam.GCPServiceAccountEmailDigest("runtime@demo-project.iam.gserviceaccount.com")

	wantAnchors := []string{network, saDigest}
	if !reflect.DeepEqual(got.CorrelationAnchors, wantAnchors) {
		t.Fatalf("anchors mismatch:\n got %#v\nwant %#v", got.CorrelationAnchors, wantAnchors)
	}

	if len(got.Relationships) != 1 {
		t.Fatalf("expected 1 edge (network), got %d: %#v", len(got.Relationships), got.Relationships)
	}
	assertRelationship(t, got.Relationships, relationshipTypeFirewallAppliesToNetwork, network, assetTypeComputeNetwork)
	for _, rel := range got.Relationships {
		if rel.SourceFullResourceName != firewallFullName {
			t.Errorf("relationship source = %q, want firewall full name", rel.SourceFullResourceName)
		}
		if rel.SourceAssetType != assetTypeComputeFirewall {
			t.Errorf("relationship source asset type = %q, want %q", rel.SourceAssetType, assetTypeComputeFirewall)
		}
		if rel.SupportState != RelationshipSupportSupported {
			t.Errorf("relationship support state = %q, want %q", rel.SupportState, RelationshipSupportSupported)
		}
	}
}

func TestExtractFirewallEgressDeny(t *testing.T) {
	const data = `{
		"network": "projects/demo-project/global/networks/prod-vpc",
		"direction": "EGRESS",
		"priority": 65535,
		"disabled": true,
		"destinationRanges": ["10.0.0.0/8", "192.168.0.0/16"],
		"denied": [{"IPProtocol": "all"}]
	}`
	got, err := extractFirewall(firewallContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Attributes["direction"] != "EGRESS" {
		t.Errorf("direction = %v, want EGRESS", got.Attributes["direction"])
	}
	if got.Attributes["disabled"] != true {
		t.Errorf("disabled = %v, want true", got.Attributes["disabled"])
	}
	if got.Attributes["destination_range_count"] != 2 {
		t.Errorf("destination_range_count = %v, want 2", got.Attributes["destination_range_count"])
	}
	if _, ok := got.Attributes["opens_to_public"]; ok {
		t.Errorf("egress with no public source must omit opens_to_public: %#v", got.Attributes)
	}
	if !reflect.DeepEqual(got.Attributes["denied_protocols"], []string{"all"}) {
		t.Errorf("denied_protocols = %#v, want [all]", got.Attributes["denied_protocols"])
	}
}

func TestExtractFirewallNeverLeaksIPRanges(t *testing.T) {
	const data = `{
		"network": "projects/p/global/networks/n",
		"direction": "INGRESS",
		"sourceRanges": ["203.0.113.7/32", "0.0.0.0/0"],
		"destinationRanges": ["198.51.100.0/24"]
	}`
	got, err := extractFirewall(firewallContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	blob, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal extraction: %v", err)
	}
	for _, forbidden := range []string{"203.0.113.7", "198.51.100"} {
		if containsString(string(blob), forbidden) {
			t.Fatalf("extraction leaked IP range %q: %s", forbidden, blob)
		}
	}
	if got.Attributes["opens_to_public"] != true {
		t.Errorf("opens_to_public = %v, want true (0.0.0.0/0 present)", got.Attributes["opens_to_public"])
	}
	if got.Attributes["source_range_count"] != 2 {
		t.Errorf("source_range_count = %v, want 2", got.Attributes["source_range_count"])
	}
}

func TestExtractFirewallEmptyDataYieldsNothing(t *testing.T) {
	got, err := extractFirewall(firewallContext(`{}`))
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

func TestExtractFirewallMalformedDataErrors(t *testing.T) {
	if _, err := extractFirewall(firewallContext(`{not json`)); err == nil {
		t.Fatalf("expected an error for malformed resource data")
	}
}

func TestFirewallOpensToPublic(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want bool
	}{
		{"ipv4 any", []string{"0.0.0.0/0"}, true},
		{"ipv6 any", []string{"::/0"}, true},
		{"mixed", []string{"10.0.0.0/8", "0.0.0.0/0"}, true},
		{"private only", []string{"10.0.0.0/8"}, false},
		{"empty", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := rangesOpenToPublic(tc.in); got != tc.want {
				t.Errorf("rangesOpenToPublic(%v) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}
