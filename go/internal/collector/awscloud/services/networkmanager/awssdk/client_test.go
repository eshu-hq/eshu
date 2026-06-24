package awssdk

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsnm "github.com/aws/aws-sdk-go-v2/service/networkmanager"
	awsnmtypes "github.com/aws/aws-sdk-go-v2/service/networkmanager/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// fakeAPI is a metadata-only Network Manager API stub. Each list/get returns one
// page so the adapter's pagination and nested fan-out can be exercised without
// the AWS SDK.
type fakeAPI struct {
	globalNetworks []awsnmtypes.GlobalNetwork
	sites          []awsnmtypes.Site
	devices        []awsnmtypes.Device
	links          []awsnmtypes.Link
	connections    []awsnmtypes.Connection
	associations   []awsnmtypes.LinkAssociation
	registrations  []awsnmtypes.TransitGatewayRegistration
	coreSummaries  []awsnmtypes.CoreNetworkSummary
	coreNetwork    *awsnmtypes.CoreNetwork
}

func (f fakeAPI) DescribeGlobalNetworks(context.Context, *awsnm.DescribeGlobalNetworksInput, ...func(*awsnm.Options)) (*awsnm.DescribeGlobalNetworksOutput, error) {
	return &awsnm.DescribeGlobalNetworksOutput{GlobalNetworks: f.globalNetworks}, nil
}

func (f fakeAPI) GetSites(context.Context, *awsnm.GetSitesInput, ...func(*awsnm.Options)) (*awsnm.GetSitesOutput, error) {
	return &awsnm.GetSitesOutput{Sites: f.sites}, nil
}

func (f fakeAPI) GetDevices(context.Context, *awsnm.GetDevicesInput, ...func(*awsnm.Options)) (*awsnm.GetDevicesOutput, error) {
	return &awsnm.GetDevicesOutput{Devices: f.devices}, nil
}

func (f fakeAPI) GetLinks(context.Context, *awsnm.GetLinksInput, ...func(*awsnm.Options)) (*awsnm.GetLinksOutput, error) {
	return &awsnm.GetLinksOutput{Links: f.links}, nil
}

func (f fakeAPI) GetConnections(context.Context, *awsnm.GetConnectionsInput, ...func(*awsnm.Options)) (*awsnm.GetConnectionsOutput, error) {
	return &awsnm.GetConnectionsOutput{Connections: f.connections}, nil
}

func (f fakeAPI) GetLinkAssociations(context.Context, *awsnm.GetLinkAssociationsInput, ...func(*awsnm.Options)) (*awsnm.GetLinkAssociationsOutput, error) {
	return &awsnm.GetLinkAssociationsOutput{LinkAssociations: f.associations}, nil
}

func (f fakeAPI) GetTransitGatewayRegistrations(context.Context, *awsnm.GetTransitGatewayRegistrationsInput, ...func(*awsnm.Options)) (*awsnm.GetTransitGatewayRegistrationsOutput, error) {
	return &awsnm.GetTransitGatewayRegistrationsOutput{TransitGatewayRegistrations: f.registrations}, nil
}

func (f fakeAPI) ListCoreNetworks(context.Context, *awsnm.ListCoreNetworksInput, ...func(*awsnm.Options)) (*awsnm.ListCoreNetworksOutput, error) {
	return &awsnm.ListCoreNetworksOutput{CoreNetworks: f.coreSummaries}, nil
}

func (f fakeAPI) GetCoreNetwork(context.Context, *awsnm.GetCoreNetworkInput, ...func(*awsnm.Options)) (*awsnm.GetCoreNetworkOutput, error) {
	return &awsnm.GetCoreNetworkOutput{CoreNetwork: f.coreNetwork}, nil
}

func TestSnapshotMapsNestedTopology(t *testing.T) {
	const gnID = "global-network-0aa11bb22cc33dd44"
	api := fakeAPI{
		globalNetworks: []awsnmtypes.GlobalNetwork{{
			GlobalNetworkArn: aws.String("arn:aws:networkmanager::123456789012:global-network/" + gnID),
			GlobalNetworkId:  aws.String(gnID),
			State:            awsnmtypes.GlobalNetworkStateAvailable,
		}},
		sites:   []awsnmtypes.Site{{SiteId: aws.String("site-1"), GlobalNetworkId: aws.String(gnID), Location: &awsnmtypes.Location{Address: aws.String("1 Main")}}},
		devices: []awsnmtypes.Device{{DeviceId: aws.String("device-1"), GlobalNetworkId: aws.String(gnID), AWSLocation: &awsnmtypes.AWSLocation{SubnetArn: aws.String("arn:aws:ec2:us-east-1:123456789012:subnet/subnet-1")}}},
		links:   []awsnmtypes.Link{{LinkId: aws.String("link-1"), GlobalNetworkId: aws.String(gnID), Bandwidth: &awsnmtypes.Bandwidth{UploadSpeed: aws.Int32(50), DownloadSpeed: aws.Int32(100)}}},
		connections: []awsnmtypes.Connection{{
			ConnectionId: aws.String("connection-1"), GlobalNetworkId: aws.String(gnID),
			DeviceId: aws.String("device-1"), ConnectedDeviceId: aws.String("device-2"),
		}},
		associations: []awsnmtypes.LinkAssociation{{GlobalNetworkId: aws.String(gnID), DeviceId: aws.String("device-1"), LinkId: aws.String("link-1")}},
		registrations: []awsnmtypes.TransitGatewayRegistration{{
			GlobalNetworkId:   aws.String(gnID),
			TransitGatewayArn: aws.String("arn:aws:ec2:us-east-1:123456789012:transit-gateway/tgw-1"),
			State:             &awsnmtypes.TransitGatewayRegistrationStateReason{Code: awsnmtypes.TransitGatewayRegistrationStateAvailable},
		}},
		coreSummaries: []awsnmtypes.CoreNetworkSummary{{CoreNetworkId: aws.String("core-network-1"), GlobalNetworkId: aws.String(gnID)}},
		coreNetwork: &awsnmtypes.CoreNetwork{
			CoreNetworkArn: aws.String("arn:aws:networkmanager::123456789012:core-network/core-network-1"),
			CoreNetworkId:  aws.String("core-network-1"), GlobalNetworkId: aws.String(gnID),
			Segments: []awsnmtypes.CoreNetworkSegment{{Name: aws.String("shared")}},
			Edges:    []awsnmtypes.CoreNetworkEdge{{EdgeLocation: aws.String("us-east-1")}},
		},
	}
	client := &Client{client: api, boundary: awscloud.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: awscloud.ServiceNetworkManager}}

	snapshot, err := client.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if len(snapshot.GlobalNetworks) != 1 {
		t.Fatalf("global networks = %d, want 1", len(snapshot.GlobalNetworks))
	}
	gn := snapshot.GlobalNetworks[0]
	if len(gn.Sites) != 1 || len(gn.Devices) != 1 || len(gn.Links) != 1 || len(gn.Connections) != 1 {
		t.Fatalf("nested children not mapped: %+v", gn)
	}
	if gn.Devices[0].SubnetARN == "" {
		t.Fatal("device SubnetARN not mapped from AWSLocation")
	}
	if gn.Links[0].UploadSpeedMbps != 50 || gn.Links[0].DownloadSpeedMbps != 100 {
		t.Fatalf("link bandwidth = %d/%d, want 50/100", gn.Links[0].UploadSpeedMbps, gn.Links[0].DownloadSpeedMbps)
	}
	if len(gn.TransitGatewayRegistrations) != 1 || gn.TransitGatewayRegistrations[0].State != "AVAILABLE" {
		t.Fatalf("tgw registration not mapped: %+v", gn.TransitGatewayRegistrations)
	}
	if len(snapshot.CoreNetworks) != 1 || snapshot.CoreNetworks[0].SegmentNames[0] != "shared" {
		t.Fatalf("core network not resolved via GetCoreNetwork: %+v", snapshot.CoreNetworks)
	}
}
