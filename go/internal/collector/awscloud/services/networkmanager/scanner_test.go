// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package networkmanager

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/internal/relguard"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

const (
	testAccount     = "123456789012"
	testGlobalNetID = "global-network-0aa11bb22cc33dd44"
	testSiteID      = "site-0aa11bb22cc33dd44"
	testDeviceID    = "device-0aa11bb22cc33dd44"
	testDevice2ID   = "device-0ff99ee88dd77cc66"
	testLinkID      = "link-0aa11bb22cc33dd44"
	testConnID      = "connection-0aa11bb22cc33dd44"
	testCoreID      = "core-network-0aa11bb22cc33dd44"
	testTGWID       = "tgw-0aa11bb22cc33dd44"

	testGlobalNetARN = "arn:aws:networkmanager::123456789012:global-network/global-network-0aa11bb22cc33dd44"
	testSiteARN      = "arn:aws:networkmanager::123456789012:site/global-network-0aa11bb22cc33dd44/site-0aa11bb22cc33dd44"
	testDeviceARN    = "arn:aws:networkmanager::123456789012:device/global-network-0aa11bb22cc33dd44/device-0aa11bb22cc33dd44"
	testLinkARN      = "arn:aws:networkmanager::123456789012:link/global-network-0aa11bb22cc33dd44/link-0aa11bb22cc33dd44"
	testConnARN      = "arn:aws:networkmanager::123456789012:connection/global-network-0aa11bb22cc33dd44/connection-0aa11bb22cc33dd44"
	testCoreARN      = "arn:aws:networkmanager::123456789012:core-network/core-network-0aa11bb22cc33dd44"
	testTGWARN       = "arn:aws:ec2:us-east-1:123456789012:transit-gateway/tgw-0aa11bb22cc33dd44"
)

func fullSnapshot() Snapshot {
	return Snapshot{
		GlobalNetworks: []GlobalNetwork{{
			ARN:       testGlobalNetARN,
			ID:        testGlobalNetID,
			State:     "AVAILABLE",
			CreatedAt: time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC),
			Tags:      map[string]string{"Environment": "prod"},
			Sites: []Site{{
				ARN:             testSiteARN,
				ID:              testSiteID,
				GlobalNetworkID: testGlobalNetID,
				State:           "ACTIVE",
				Address:         "123 Main St",
			}},
			Devices: []Device{{
				ARN:             testDeviceARN,
				ID:              testDeviceID,
				GlobalNetworkID: testGlobalNetID,
				SiteID:          testSiteID,
				Vendor:          "Cisco",
				SubnetARN:       "arn:aws:ec2:us-east-1:123456789012:subnet/subnet-0aa11bb22cc33dd44",
				State:           "AVAILABLE",
			}},
			Links: []Link{{
				ARN:               testLinkARN,
				ID:                testLinkID,
				GlobalNetworkID:   testGlobalNetID,
				SiteID:            testSiteID,
				Provider:          "ExampleTelco",
				UploadSpeedMbps:   100,
				DownloadSpeedMbps: 200,
				State:             "AVAILABLE",
			}},
			Connections: []Connection{{
				ARN:               testConnARN,
				ID:                testConnID,
				GlobalNetworkID:   testGlobalNetID,
				DeviceID:          testDeviceID,
				ConnectedDeviceID: testDevice2ID,
				LinkID:            testLinkID,
				State:             "AVAILABLE",
			}},
			LinkAssociations: []LinkAssociation{{
				GlobalNetworkID: testGlobalNetID,
				DeviceID:        testDeviceID,
				LinkID:          testLinkID,
				State:           "LINK_ASSOCIATED",
			}},
			TransitGatewayRegistrations: []TransitGatewayRegistration{{
				GlobalNetworkID:   testGlobalNetID,
				TransitGatewayARN: testTGWARN,
				State:             "AVAILABLE",
			}},
		}},
		CoreNetworks: []CoreNetwork{{
			ARN:             testCoreARN,
			ID:              testCoreID,
			GlobalNetworkID: testGlobalNetID,
			State:           "AVAILABLE",
			SegmentNames:    []string{"shared"},
			EdgeLocations:   []string{"us-east-1"},
		}},
	}
}

func TestScannerEmitsNetworkManagerResources(t *testing.T) {
	envelopes := scan(t, testBoundary(), fakeClient{snapshot: fullSnapshot()})

	cases := []struct {
		resourceType string
		resourceID   string
	}{
		{awscloud.ResourceTypeNetworkManagerGlobalNetwork, testGlobalNetARN},
		{awscloud.ResourceTypeNetworkManagerSite, testSiteARN},
		{awscloud.ResourceTypeNetworkManagerDevice, testDeviceARN},
		{awscloud.ResourceTypeNetworkManagerLink, testLinkARN},
		{awscloud.ResourceTypeNetworkManagerConnection, testConnARN},
		{awscloud.ResourceTypeNetworkManagerCoreNetwork, testCoreARN},
	}
	for _, tc := range cases {
		resource := resourceByType(t, envelopes, tc.resourceType)
		if got := resource.Payload["resource_id"]; got != tc.resourceID {
			t.Fatalf("%s resource_id = %#v, want %q", tc.resourceType, got, tc.resourceID)
		}
		if got := resource.Payload["arn"]; got != tc.resourceID {
			t.Fatalf("%s arn = %#v, want %q", tc.resourceType, got, tc.resourceID)
		}
	}

	device := resourceByType(t, envelopes, awscloud.ResourceTypeNetworkManagerDevice)
	deviceAttrs := attributesOf(t, device)
	assertAttribute(t, deviceAttrs, "vendor", "Cisco")
	assertAttribute(t, deviceAttrs, "subnet_arn", "arn:aws:ec2:us-east-1:123456789012:subnet/subnet-0aa11bb22cc33dd44")

	link := resourceByType(t, envelopes, awscloud.ResourceTypeNetworkManagerLink)
	linkAttrs := attributesOf(t, link)
	assertAttribute(t, linkAttrs, "upload_speed_mbps", int32(100))
	assertAttribute(t, linkAttrs, "download_speed_mbps", int32(200))

	core := resourceByType(t, envelopes, awscloud.ResourceTypeNetworkManagerCoreNetwork)
	coreAttrs := attributesOf(t, core)
	assertAttribute(t, coreAttrs, "segment_names", []string{"shared"})
}

func TestScannerEmitsNetworkManagerEdges(t *testing.T) {
	envelopes := scan(t, testBoundary(), fakeClient{snapshot: fullSnapshot()})

	// core network -> global network
	core := relationshipByType(t, envelopes, awscloud.RelationshipNetworkManagerCoreNetworkInGlobalNetwork)
	assertEdgeTarget(t, core, awscloud.ResourceTypeNetworkManagerGlobalNetwork, testGlobalNetARN)
	if got := core.Payload["source_resource_id"]; got != testCoreARN {
		t.Fatalf("core->gn source_resource_id = %#v, want %q", got, testCoreARN)
	}

	// site/device/link/connection -> global network
	for _, relType := range []string{
		awscloud.RelationshipNetworkManagerSiteInGlobalNetwork,
		awscloud.RelationshipNetworkManagerDeviceInGlobalNetwork,
		awscloud.RelationshipNetworkManagerLinkInGlobalNetwork,
		awscloud.RelationshipNetworkManagerConnectionInGlobalNetwork,
	} {
		rel := relationshipByType(t, envelopes, relType)
		assertEdgeTarget(t, rel, awscloud.ResourceTypeNetworkManagerGlobalNetwork, testGlobalNetARN)
	}

	// device -> site, link -> site
	deviceSite := relationshipByType(t, envelopes, awscloud.RelationshipNetworkManagerDeviceInSite)
	assertEdgeTarget(t, deviceSite, awscloud.ResourceTypeNetworkManagerSite, testSiteARN)
	linkSite := relationshipByType(t, envelopes, awscloud.RelationshipNetworkManagerLinkInSite)
	assertEdgeTarget(t, linkSite, awscloud.ResourceTypeNetworkManagerSite, testSiteARN)

	// device -> link association
	deviceLink := relationshipByType(t, envelopes, awscloud.RelationshipNetworkManagerDeviceUsesLink)
	assertEdgeTarget(t, deviceLink, awscloud.ResourceTypeNetworkManagerLink, testLinkARN)
	if got := deviceLink.Payload["source_resource_id"]; got != testDeviceARN {
		t.Fatalf("device->link source_resource_id = %#v, want %q", got, testDeviceARN)
	}

	// connection -> device (first and connected device)
	connDevices := relationshipsByType(envelopes, awscloud.RelationshipNetworkManagerConnectionConnectsDevice)
	if len(connDevices) != 2 {
		t.Fatalf("connection->device edges = %d, want 2", len(connDevices))
	}

	// transit gateway registration: gn -> transit gateway, keyed by BARE tgw id
	tgw := relationshipByType(t, envelopes, awscloud.RelationshipNetworkManagerGlobalNetworkRegistersTransitGateway)
	assertEdgeTarget(t, tgw, awscloud.ResourceTypeTransitGateway, testTGWID)
	if got := tgw.Payload["source_resource_id"]; got != testGlobalNetARN {
		t.Fatalf("tgw registration source_resource_id = %#v, want %q", got, testGlobalNetARN)
	}
	if got := tgw.Payload["target_arn"]; got != "" {
		t.Fatalf("tgw registration target_arn = %#v, want empty (target keyed by bare id)", got)
	}
}

func TestScannerRelationshipsSatisfyGraphJoinGuard(t *testing.T) {
	envelopes := scan(t, testBoundary(), fakeClient{snapshot: fullSnapshot()})
	var observations []awscloud.RelationshipObservation
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		observations = append(observations, relationshipObservationFrom(t, envelope))
	}
	if len(observations) == 0 {
		t.Fatal("expected relationship observations to assert")
	}
	relguard.AssertObservations(t, observations...)
}

func TestScannerSynthesizesGovCloudParentEdge(t *testing.T) {
	boundary := testBoundary()
	boundary.Region = "us-gov-west-1"
	// Child reports only ids; the scanner synthesizes a partition-aware parent ARN.
	snapshot := Snapshot{GlobalNetworks: []GlobalNetwork{{
		ID:    testGlobalNetID,
		State: "AVAILABLE",
		Sites: []Site{{ID: testSiteID, GlobalNetworkID: testGlobalNetID, State: "ACTIVE"}},
	}}}
	envelopes := scan(t, boundary, fakeClient{snapshot: snapshot})
	site := relationshipByType(t, envelopes, awscloud.RelationshipNetworkManagerSiteInGlobalNetwork)
	wantARN := "arn:aws-us-gov:networkmanager::123456789012:global-network/" + testGlobalNetID
	if got := site.Payload["target_resource_id"]; got != wantARN {
		t.Fatalf("GovCloud site->gn target_resource_id = %#v, want %q", got, wantARN)
	}
}

func TestScannerOmitsEdgesWhenDependenciesAbsent(t *testing.T) {
	// A global network with no children and no registrations emits only its node.
	envelopes := scan(t, testBoundary(), fakeClient{snapshot: Snapshot{
		GlobalNetworks: []GlobalNetwork{{ARN: testGlobalNetARN, ID: testGlobalNetID, State: "AVAILABLE"}},
	}})
	for _, envelope := range envelopes {
		if envelope.FactKind == facts.AWSRelationshipFactKind {
			t.Fatalf("unexpected relationship emitted: %#v", envelope.Payload)
		}
	}
}

func TestScannerStaysMetadataOnly(t *testing.T) {
	envelopes := scan(t, testBoundary(), fakeClient{snapshot: fullSnapshot()})
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		attrs, _ := envelope.Payload["attributes"].(map[string]any)
		for _, forbidden := range []string{
			"serial_number", "routing_policy", "policy_document",
			"network_routes", "route_analysis", "telemetry",
		} {
			if _, exists := attrs[forbidden]; exists {
				t.Fatalf("%s attribute persisted; Network Manager scanner must stay metadata-only", forbidden)
			}
		}
	}
}

func TestScannerRejectsMismatchedServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = awscloud.ServiceS3
	if _, err := (Scanner{Client: fakeClient{}}).Scan(context.Background(), boundary); err == nil {
		t.Fatal("Scan() error = nil, want service kind mismatch")
	}
}

func TestScannerEmitsThrottleWarningFacts(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{
		GlobalNetworks: []GlobalNetwork{{ARN: testGlobalNetARN, ID: testGlobalNetID, State: "AVAILABLE"}},
		Warnings: []awscloud.WarningObservation{{
			Boundary:       testBoundary(),
			WarningKind:    awscloud.WarningThrottleSustained,
			ErrorClass:     "throttled",
			Message:        "Network Manager GetDevices throttled after SDK retries; device metadata omitted",
			SourceRecordID: "networkmanager_devices_throttled",
		}},
	}}
	envelopes := scan(t, testBoundary(), client)
	warning := warningByKind(t, envelopes, awscloud.WarningThrottleSustained)
	if got := warning.Payload["error_class"]; got != "throttled" {
		t.Fatalf("warning error_class = %#v, want throttled", got)
	}
}

func TestScannerRequiresClient(t *testing.T) {
	if _, err := (Scanner{}).Scan(context.Background(), testBoundary()); err == nil {
		t.Fatal("Scan() error = nil, want missing-client error")
	}
}
