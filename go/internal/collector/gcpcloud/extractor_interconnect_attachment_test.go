// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"testing"
)

const interconnectAttachmentFullName = "//compute.googleapis.com/projects/demo-project/regions/us-central1/interconnectAttachments/edge-attach"

func interconnectAttachmentContext(data string) ExtractContext {
	return ExtractContext{
		FullResourceName: interconnectAttachmentFullName,
		AssetType:        assetTypeComputeInterconnectAttachment,
		ProjectID:        "demo-project",
		Data:             json.RawMessage(data),
	}
}

func TestInterconnectAttachmentExtractorIsRegistered(t *testing.T) {
	if _, ok := lookupAssetExtractor(assetTypeComputeInterconnectAttachment); !ok {
		t.Fatalf("expected %q extractor to self-register via init()", assetTypeComputeInterconnectAttachment)
	}
}

func TestExtractInterconnectAttachmentBasicAttributesAndEdges(t *testing.T) {
	const data = `{
		"name": "edge-attach",
		"region": "https://www.googleapis.com/compute/v1/projects/demo-project/regions/us-central1",
		"router": "https://www.googleapis.com/compute/v1/projects/demo-project/regions/us-central1/routers/edge-router",
		"interconnect": "https://www.googleapis.com/compute/v1/projects/demo-project/global/interconnects/edge-interconnect",
		"type": "DEDICATED",
		"bandwidth": "BPS_10G",
		"edgeAvailabilityDomain": "AVAILABILITY_DOMAIN_1",
		"state": "ACTIVE",
		"creationTimestamp": "2024-06-01T00:00:00.000-07:00"
	}`

	got, err := extractInterconnectAttachment(interconnectAttachmentContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantAttrs := map[string]any{
		"region":                   "us-central1",
		"type":                     "DEDICATED",
		"bandwidth":                "BPS_10G",
		"edge_availability_domain": "AVAILABILITY_DOMAIN_1",
		"state":                    "ACTIVE",
		"creation_time":            "2024-06-01T07:00:00Z",
	}
	for k, v := range wantAttrs {
		if got.Attributes[k] != v {
			t.Errorf("attributes[%q] = %#v, want %#v", k, got.Attributes[k], v)
		}
	}

	const router = "//compute.googleapis.com/projects/demo-project/regions/us-central1/routers/edge-router"
	const interconnect = "//compute.googleapis.com/projects/demo-project/global/interconnects/edge-interconnect"
	assertRelationship(t, got.Relationships, relationshipTypeInterconnectAttachmentUsesRouter, router, assetTypeComputeRouter)
	assertRelationship(t, got.Relationships, relationshipTypeInterconnectAttachmentUsesInterconnect, interconnect, assetTypeComputeInterconnect)

	for _, anchor := range []string{router, interconnect} {
		if !containsStringSlice(got.CorrelationAnchors, anchor) {
			t.Errorf("expected anchor %q in %#v", anchor, got.CorrelationAnchors)
		}
	}
}

func TestExtractInterconnectAttachmentPartnerAsn(t *testing.T) {
	const data = `{
		"region": "us-central1",
		"type": "PARTNER",
		"partnerAsn": "65001"
	}`

	got, err := extractInterconnectAttachment(interconnectAttachmentContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Attributes["partner_asn"] != int64(65001) {
		t.Errorf("partner_asn = %#v, want 65001", got.Attributes["partner_asn"])
	}
}

func TestExtractInterconnectAttachmentPartnerAsnAbsentOmitted(t *testing.T) {
	// A field CAI omits entirely (not present-0) must never be fabricated as a
	// zero value on the attribute map.
	const data = `{"region": "us-central1", "type": "DEDICATED"}`

	got, err := extractInterconnectAttachment(interconnectAttachmentContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, present := got.Attributes["partner_asn"]; present {
		t.Errorf("partner_asn must be omitted when absent, got %#v", got.Attributes["partner_asn"])
	}
}

func TestExtractInterconnectAttachmentRejectsWrongSegmentReferences(t *testing.T) {
	// A malformed or anomalous CAI page could put a wrong-kind selfLink in
	// router or interconnect. Segment validation must reject it rather than
	// emit an edge with a fabricated target_type.
	const data = `{
		"region": "us-central1",
		"router": "projects/demo-project/regions/us-central1/subnetworks/sub-1",
		"interconnect": "projects/demo-project/regions/us-central1/routers/edge-router"
	}`

	got, err := extractInterconnectAttachment(interconnectAttachmentContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Relationships) != 0 {
		t.Fatalf("expected no edges for wrong-segment references, got %#v", got.Relationships)
	}
	if len(got.CorrelationAnchors) != 0 {
		t.Fatalf("expected no anchors for wrong-segment references, got %#v", got.CorrelationAnchors)
	}
}

func TestExtractInterconnectAttachmentEmptyDataYieldsNothing(t *testing.T) {
	got, err := extractInterconnectAttachment(interconnectAttachmentContext(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Attributes) != 0 || len(got.Relationships) != 0 || len(got.CorrelationAnchors) != 0 {
		t.Errorf("expected nothing for empty data, got %#v", got)
	}
}

func TestExtractInterconnectAttachmentMalformedDataErrors(t *testing.T) {
	if _, err := extractInterconnectAttachment(interconnectAttachmentContext(`{not json`)); err == nil {
		t.Fatalf("expected an error for malformed resource data")
	}
}

func TestExtractInterconnectAttachmentNeverLeaksIPOrLabelData(t *testing.T) {
	// candidateCloudRouterIpAddress, cloudRouterIpAddress,
	// customerRouterIpAddress, and their IPv6 counterparts must never be
	// decoded: the struct declares no fields for them, so encoding/json
	// silently ignores these keys during Unmarshal.
	const data = `{
		"region": "us-central1",
		"candidateCloudRouterIpAddress": "169.254.0.1/29",
		"cloudRouterIpAddress": "169.254.0.2/29",
		"customerRouterIpAddress": "169.254.0.3/29",
		"labels": {"env": "prod"}
	}`

	got, err := extractInterconnectAttachment(interconnectAttachmentContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	blob, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal extraction: %v", err)
	}
	for _, forbidden := range []string{"169.254.0.1", "169.254.0.2", "169.254.0.3", "prod"} {
		if containsString(string(blob), forbidden) {
			t.Fatalf("extraction leaked forbidden token %q: %s", forbidden, blob)
		}
	}
}
