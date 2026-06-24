// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package outposts

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/internal/relguard"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

const (
	testOutpostARN = "arn:aws:outposts:us-east-1:123456789012:outpost/op-0123456789abcdef0"
	testSiteARN    = "arn:aws:outposts:us-east-1:123456789012:site/os-0123456789abcdef0"
)

func testRackElevation() *float64 {
	value := 14.0
	return &value
}

func TestScannerEmitsOutpostsMetadataAndRelationships(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{
		Outposts: []Outpost{{
			ARN:                   testOutpostARN,
			OutpostID:             "op-0123456789abcdef0",
			Name:                  "edge-rack-1",
			Description:           "Primary rack",
			LifeCycleStatus:       "ACTIVE",
			AvailabilityZone:      "us-east-1a",
			AvailabilityZoneID:    "use1-az1",
			OwnerID:               "123456789012",
			SiteID:                "os-0123456789abcdef0",
			SiteARN:               testSiteARN,
			SupportedHardwareType: "RACK",
			Tags:                  map[string]string{"Environment": "prod"},
			Assets: []Asset{{
				AssetID:       "asset-1234",
				AssetType:     "COMPUTE",
				RackID:        "rack-5678",
				ComputeState:  "ACTIVE",
				RackElevation: testRackElevation(),
			}},
		}},
		Sites: []Site{{
			ARN:       testSiteARN,
			SiteID:    "os-0123456789abcdef0",
			Name:      "datacenter-east",
			AccountID: "123456789012",
			Tags:      map[string]string{"Team": "infra"},
		}},
	}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	// Outpost resource node.
	outpost := resourceByType(t, envelopes, awscloud.ResourceTypeOutpostsOutpost)
	if got, want := outpost.Payload["resource_id"], testOutpostARN; got != want {
		t.Fatalf("outpost resource_id = %#v, want %q", got, want)
	}
	if got, want := outpost.Payload["arn"], testOutpostARN; got != want {
		t.Fatalf("outpost arn = %#v, want %q", got, want)
	}
	if got, want := outpost.Payload["state"], "ACTIVE"; got != want {
		t.Fatalf("outpost state = %#v, want %q", got, want)
	}
	outpostAttrs := attributesOf(t, outpost)
	assertAttribute(t, outpostAttrs, "outpost_id", "op-0123456789abcdef0")
	assertAttribute(t, outpostAttrs, "availability_zone", "us-east-1a")
	assertAttribute(t, outpostAttrs, "availability_zone_id", "use1-az1")
	assertAttribute(t, outpostAttrs, "owner_id", "123456789012")
	assertAttribute(t, outpostAttrs, "site_id", "os-0123456789abcdef0")
	assertAttribute(t, outpostAttrs, "supported_hardware_type", "RACK")

	// Site resource node.
	site := resourceByType(t, envelopes, awscloud.ResourceTypeOutpostsSite)
	if got, want := site.Payload["resource_id"], testSiteARN; got != want {
		t.Fatalf("site resource_id = %#v, want %q", got, want)
	}
	siteAttrs := attributesOf(t, site)
	assertAttribute(t, siteAttrs, "site_id", "os-0123456789abcdef0")
	assertAttribute(t, siteAttrs, "account_id", "123456789012")

	// Asset resource node.
	asset := resourceByType(t, envelopes, awscloud.ResourceTypeOutpostsAsset)
	wantAssetID := testOutpostARN + "/asset/asset-1234"
	if got, want := asset.Payload["resource_id"], wantAssetID; got != want {
		t.Fatalf("asset resource_id = %#v, want %q", got, want)
	}
	if got, want := asset.Payload["state"], "ACTIVE"; got != want {
		t.Fatalf("asset state = %#v, want %q", got, want)
	}
	assetAttrs := attributesOf(t, asset)
	assertAttribute(t, assetAttrs, "asset_id", "asset-1234")
	assertAttribute(t, assetAttrs, "asset_type", "COMPUTE")
	assertAttribute(t, assetAttrs, "rack_id", "rack-5678")
	assertAttribute(t, assetAttrs, "rack_elevation", 14.0)

	// Outpost -> site edge, keyed by the site ARN the site node publishes.
	outpostInSite := relationshipByType(t, envelopes, awscloud.RelationshipOutpostsOutpostInSite)
	assertEdgeTarget(t, outpostInSite, awscloud.ResourceTypeOutpostsSite, testSiteARN)
	if got, want := outpostInSite.Payload["source_resource_id"], testOutpostARN; got != want {
		t.Fatalf("outpost->site source_resource_id = %#v, want %q", got, want)
	}
	if got, want := outpostInSite.Payload["target_arn"], testSiteARN; got != want {
		t.Fatalf("outpost->site target_arn = %#v, want %q", got, want)
	}

	// Asset -> outpost edge, keyed by the outpost ARN the outpost node publishes.
	assetInOutpost := relationshipByType(t, envelopes, awscloud.RelationshipOutpostsAssetInOutpost)
	assertEdgeTarget(t, assetInOutpost, awscloud.ResourceTypeOutpostsOutpost, testOutpostARN)
	if got, want := assetInOutpost.Payload["source_resource_id"], wantAssetID; got != want {
		t.Fatalf("asset->outpost source_resource_id = %#v, want %q", got, want)
	}
	if got, want := assetInOutpost.Payload["target_arn"], testOutpostARN; got != want {
		t.Fatalf("asset->outpost target_arn = %#v, want %q", got, want)
	}

	// No physical-address or logistics leakage anywhere in the resource payloads.
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		attrs, _ := envelope.Payload["attributes"].(map[string]any)
		for _, forbidden := range []string{
			"operating_address", "operating_address_city", "operating_address_country_code",
			"operating_address_state_or_region", "country_code", "notes", "street_address",
			"shipping_address", "contact", "rack_physical_properties", "power_draw_kva",
		} {
			if _, exists := attrs[forbidden]; exists {
				t.Fatalf("%s attribute persisted; Outposts scanner must never persist physical site addresses or logistics", forbidden)
			}
		}
	}
}

func TestScannerOmitsRelationshipsWhenEndpointsAbsent(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{
		Outposts: []Outpost{{
			ARN:       testOutpostARN,
			OutpostID: "op-0123456789abcdef0",
			Name:      "edge-rack-1",
			// No SiteARN/SiteID, no assets: no edges.
		}},
	}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	for _, envelope := range envelopes {
		if envelope.FactKind == facts.AWSRelationshipFactKind {
			t.Fatalf("unexpected relationship emitted: %#v", envelope.Payload)
		}
	}
}

func TestScannerSynthesizesGovCloudAssetID(t *testing.T) {
	boundary := testBoundary()
	boundary.Region = "us-gov-west-1"
	govOutpostARN := "arn:aws-us-gov:outposts:us-gov-west-1:123456789012:outpost/op-gov"
	client := fakeClient{snapshot: Snapshot{
		Outposts: []Outpost{{
			ARN:       govOutpostARN,
			OutpostID: "op-gov",
			Name:      "gov-rack",
			Assets: []Asset{{
				AssetID:   "asset-gov",
				AssetType: "COMPUTE",
			}},
		}},
	}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), boundary)
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	asset := resourceByType(t, envelopes, awscloud.ResourceTypeOutpostsAsset)
	wantAssetID := govOutpostARN + "/asset/asset-gov"
	if got := asset.Payload["resource_id"]; got != wantAssetID {
		t.Fatalf("GovCloud asset resource_id = %#v, want %q", got, wantAssetID)
	}
	assetInOutpost := relationshipByType(t, envelopes, awscloud.RelationshipOutpostsAssetInOutpost)
	assertEdgeTarget(t, assetInOutpost, awscloud.ResourceTypeOutpostsOutpost, govOutpostARN)
}

func TestScannerRelationshipsSatisfyGraphJoinGuard(t *testing.T) {
	boundary := testBoundary()
	outpost := Outpost{
		ARN:       testOutpostARN,
		OutpostID: "op-0123456789abcdef0",
		SiteARN:   testSiteARN,
		SiteID:    "os-0123456789abcdef0",
	}
	asset := Asset{AssetID: "asset-1234", AssetType: "COMPUTE", RackID: "rack-5678"}
	assetID := assetResourceID(outpost, asset)
	var observations []awscloud.RelationshipObservation
	for _, rel := range []*awscloud.RelationshipObservation{
		outpostInSiteRelationship(boundary, outpost),
		assetInOutpostRelationship(boundary, outpost, assetID),
	} {
		if rel == nil {
			t.Fatalf("expected non-nil relationship for fully populated fixture")
		}
		observations = append(observations, *rel)
	}
	relguard.AssertObservations(t, observations...)
}

func TestScannerRejectsMismatchedServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = awscloud.ServiceS3

	_, err := (Scanner{Client: fakeClient{}}).Scan(context.Background(), boundary)
	if err == nil {
		t.Fatalf("Scan() error = nil, want service kind mismatch")
	}
}

func TestScannerEmitsThrottleWarningFacts(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{
		Outposts: []Outpost{{ARN: testOutpostARN, OutpostID: "op-0123456789abcdef0"}},
		Warnings: []awscloud.WarningObservation{{
			Boundary:       testBoundary(),
			WarningKind:    awscloud.WarningThrottleSustained,
			ErrorClass:     "throttled",
			Message:        "Outposts ListAssets throttled after SDK retries; asset metadata omitted for this scan",
			SourceRecordID: "outposts_assets_throttled",
		}},
	}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	warning := warningByKind(t, envelopes, awscloud.WarningThrottleSustained)
	if got := warning.Payload["error_class"]; got != "throttled" {
		t.Fatalf("warning error_class = %#v, want throttled", got)
	}
}

func TestScannerRequiresClient(t *testing.T) {
	_, err := (Scanner{}).Scan(context.Background(), testBoundary())
	if err == nil {
		t.Fatalf("Scan() error = nil, want client-required error")
	}
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceOutposts,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:outposts:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        42,
		ObservedAt:          time.Date(2026, 5, 14, 18, 30, 0, 0, time.UTC),
	}
}

type fakeClient struct {
	snapshot Snapshot
}

func (c fakeClient) Snapshot(context.Context) (Snapshot, error) {
	return c.snapshot, nil
}

func resourceByType(t *testing.T, envelopes []facts.Envelope, resourceType string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		if got, _ := envelope.Payload["resource_type"].(string); got == resourceType {
			return envelope
		}
	}
	t.Fatalf("missing resource_type %q in %#v", resourceType, envelopes)
	return facts.Envelope{}
}

func relationshipByType(t *testing.T, envelopes []facts.Envelope, relationshipType string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got == relationshipType {
			return envelope
		}
	}
	t.Fatalf("missing relationship_type %q in %#v", relationshipType, envelopes)
	return facts.Envelope{}
}

func warningByKind(t *testing.T, envelopes []facts.Envelope, warningKind string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSWarningFactKind {
			continue
		}
		if got, _ := envelope.Payload["warning_kind"].(string); got == warningKind {
			return envelope
		}
	}
	t.Fatalf("missing warning_kind %q in %#v", warningKind, envelopes)
	return facts.Envelope{}
}

func assertEdgeTarget(t *testing.T, envelope facts.Envelope, targetType, targetResourceID string) {
	t.Helper()
	if got := envelope.Payload["target_type"]; got != targetType {
		t.Fatalf("target_type = %#v, want %q", got, targetType)
	}
	if got := envelope.Payload["target_resource_id"]; got != targetResourceID {
		t.Fatalf("target_resource_id = %#v, want %q", got, targetResourceID)
	}
}

func attributesOf(t *testing.T, envelope facts.Envelope) map[string]any {
	t.Helper()
	attributes, ok := envelope.Payload["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("attributes = %#v, want map", envelope.Payload["attributes"])
	}
	return attributes
}

func assertAttribute(t *testing.T, attributes map[string]any, key string, want any) {
	t.Helper()
	got, exists := attributes[key]
	if !exists {
		t.Fatalf("missing attribute %q in %#v", key, attributes)
	}
	if got != want {
		t.Fatalf("attribute %q = %#v, want %#v", key, got, want)
	}
}
