// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

// TestAPIClientNeverIncludesForbiddenMethods locks the SDK adapter's narrow
// apiClient interface against any AWS EC2 method that would let the VPC
// scanner mutate network-fabric resources or enable rule evaluation paths the
// issue explicitly forbids. Adding any of these names to apiClient (whether
// directly or by embedding a new AWS SDK paginator interface that pulls them
// in) fails this test before the change can be compiled into production.
func TestAPIClientNeverIncludesForbiddenMethods(t *testing.T) {
	apiClientType := reflect.TypeOf((*apiClient)(nil)).Elem()
	forbidden := []string{
		// Issue #731 explicit blocklist.
		"CreateVpc",
		"DeleteVpc",
		"ModifyVpcAttribute",
		"CreateSubnet",
		"DeleteSubnet",
		"ModifySubnetAttribute",
		"CreateRouteTable",
		"DeleteRouteTable",
		"CreateInternetGateway",
		"DeleteInternetGateway",
		"CreateNatGateway",
		"DeleteNatGateway",
		"CreateNetworkAcl",
		"DeleteNetworkAcl",
		"CreateVpcPeeringConnection",
		"DeleteVpcPeeringConnection",
		"CreateVpcEndpoint",
		"DeleteVpcEndpoint",
		"ModifyVpcEndpoint",
		"AssociateRouteTable",
		"DisassociateRouteTable",
		"AuthorizeSecurityGroupIngress",
		"AuthorizeSecurityGroupEgress",
		"RevokeSecurityGroupIngress",
		"RevokeSecurityGroupEgress",
		"AllocateAddress",
		"ReleaseAddress",
		"AssociateAddress",
		"DisassociateAddress",
		// Mutation surface on adjacent VPC resources that must also stay off.
		"CreateRoute",
		"DeleteRoute",
		"ReplaceRoute",
		"AcceptVpcPeeringConnection",
		"RejectVpcPeeringConnection",
		"AttachInternetGateway",
		"DetachInternetGateway",
		"AttachVpnGateway",
		"DetachVpnGateway",
		"CreateVpnGateway",
		"DeleteVpnGateway",
		"CreateVpnConnection",
		"DeleteVpnConnection",
		"ModifyVpnConnection",
		"CreateCustomerGateway",
		"DeleteCustomerGateway",
		"CreateDhcpOptions",
		"DeleteDhcpOptions",
		"AssociateDhcpOptions",
		"CreateNetworkAclEntry",
		"DeleteNetworkAclEntry",
		"ReplaceNetworkAclEntry",
		"CreateTransitGateway",
		"DeleteTransitGateway",
		"AttachTransitGatewayVpcAttachment",
		"AssociateTransitGatewayRouteTable",
		"TagResources",
		"TagResource",
		"UntagResources",
		"UntagResource",
		"CreateTags",
		"DeleteTags",
		"ResetSnapshotAttribute",
	}
	for _, method := range forbidden {
		if _, ok := apiClientType.MethodByName(method); ok {
			t.Fatalf("apiClient declares forbidden EC2/VPC API %q; the adapter must not gain access to that method", method)
		}
	}
}

// TestAPIClientOnlyReadsListsAndDescribes locks the adapter's method set so a
// future change that adds, say, ModifyVpcEndpoint by embedding a new SDK
// interface fails the test even if it does not appear in the forbidden list.
func TestAPIClientOnlyReadsListsAndDescribes(t *testing.T) {
	apiClientType := reflect.TypeOf((*apiClient)(nil)).Elem()
	for i := 0; i < apiClientType.NumMethod(); i++ {
		method := apiClientType.Method(i)
		if !strings.HasPrefix(method.Name, "Describe") && !strings.HasPrefix(method.Name, "Get") && !strings.HasPrefix(method.Name, "List") {
			t.Fatalf("apiClient method %q is not a Describe/Get/List read", method.Name)
		}
	}
}

func TestMapRouteTableMapsAssociationsAndRoutes(t *testing.T) {
	rt := mapRouteTable(awsec2types.RouteTable{
		RouteTableId: aws.String("rtb-1"),
		VpcId:        aws.String("vpc-1"),
		OwnerId:      aws.String("123456789012"),
		Associations: []awsec2types.RouteTableAssociation{{
			RouteTableAssociationId: aws.String("rtbassoc-1"),
			SubnetId:                aws.String("subnet-1"),
			Main:                    aws.Bool(false),
			AssociationState: &awsec2types.RouteTableAssociationState{
				State: awsec2types.RouteTableAssociationStateCodeAssociated,
			},
		}},
		Routes: []awsec2types.Route{{
			DestinationCidrBlock: aws.String("0.0.0.0/0"),
			GatewayId:            aws.String("igw-1"),
			Origin:               awsec2types.RouteOriginCreateRoute,
			State:                awsec2types.RouteStateActive,
		}},
		Tags: []awsec2types.Tag{{Key: aws.String("env"), Value: aws.String("prod")}},
	})

	if rt.ID != "rtb-1" {
		t.Fatalf("ID = %q", rt.ID)
	}
	if rt.Tags["env"] != "prod" {
		t.Fatalf("tag env = %q", rt.Tags["env"])
	}
	if len(rt.Associations) != 1 || rt.Associations[0].SubnetID != "subnet-1" {
		t.Fatalf("associations = %#v", rt.Associations)
	}
	if rt.Associations[0].State != "associated" {
		t.Fatalf("association state = %q", rt.Associations[0].State)
	}
	if len(rt.Routes) != 1 || rt.Routes[0].GatewayID != "igw-1" {
		t.Fatalf("routes = %#v", rt.Routes)
	}
	if rt.Routes[0].Origin != "CreateRoute" || rt.Routes[0].State != "active" {
		t.Fatalf("route origin/state = %q/%q", rt.Routes[0].Origin, rt.Routes[0].State)
	}
}

// TestMapRoutePreservesVPCEndpointTarget guards the SDK adapter against dropping
// gateway-endpoint route targets. AWS does not expose a dedicated VpcEndpointId
// field on a route; a gateway VPC endpoint (for example an S3 gateway endpoint)
// appears as a managed prefix-list route whose GatewayId carries the vpce-
// prefixed endpoint ID. mapRoute must steer that vpce- target into
// Route.VPCEndpointID, otherwise the scanner never emits the
// vpc_route_targets_vpc_endpoint relationship even though the relationship
// builder and scanner tests expect Route.VPCEndpointID to be populated. The
// endpoint target must not leak into Route.GatewayID, where the internet-gateway
// relationship builder's igw- prefix guard would otherwise have to defend
// against it.
func TestMapRoutePreservesVPCEndpointTarget(t *testing.T) {
	rt := mapRouteTable(awsec2types.RouteTable{
		RouteTableId: aws.String("rtb-1"),
		VpcId:        aws.String("vpc-1"),
		Routes: []awsec2types.Route{{
			DestinationPrefixListId: aws.String("pl-1"),
			GatewayId:               aws.String("vpce-1"),
			Origin:                  awsec2types.RouteOriginCreateRoute,
			State:                   awsec2types.RouteStateActive,
		}},
	})

	if len(rt.Routes) != 1 {
		t.Fatalf("routes = %#v", rt.Routes)
	}
	if rt.Routes[0].VPCEndpointID != "vpce-1" {
		t.Fatalf("VPCEndpointID = %q, want %q", rt.Routes[0].VPCEndpointID, "vpce-1")
	}
	if rt.Routes[0].GatewayID != "" {
		t.Fatalf("GatewayID = %q, want empty for a vpce- endpoint target", rt.Routes[0].GatewayID)
	}
}

func TestMapVPCEndpointMapsSubnetsRouteTablesAndGroups(t *testing.T) {
	endpoint := mapVPCEndpoint(awsec2types.VpcEndpoint{
		VpcEndpointId:       aws.String("vpce-1"),
		VpcId:               aws.String("vpc-1"),
		ServiceName:         aws.String("com.amazonaws.us-east-1.s3"),
		VpcEndpointType:     awsec2types.VpcEndpointTypeGateway,
		State:               awsec2types.StateAvailable,
		PrivateDnsEnabled:   aws.Bool(false),
		RequesterManaged:    aws.Bool(false),
		OwnerId:             aws.String("123456789012"),
		RouteTableIds:       []string{"rtb-1"},
		SubnetIds:           []string{"subnet-1"},
		NetworkInterfaceIds: []string{"eni-1"},
		Groups: []awsec2types.SecurityGroupIdentifier{{
			GroupId:   aws.String("sg-1"),
			GroupName: aws.String("default"),
		}},
		DnsEntries: []awsec2types.DnsEntry{{
			DnsName:      aws.String("vpce-1.s3.us-east-1.vpce.amazonaws.com"),
			HostedZoneId: aws.String("Z0123456789"),
		}},
		CreationTimestamp: aws.Time(time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)),
		Tags:              []awsec2types.Tag{{Key: aws.String("Name"), Value: aws.String("s3-endpoint")}},
	})

	if endpoint.EndpointType != "Gateway" {
		t.Fatalf("endpoint type = %q", endpoint.EndpointType)
	}
	if len(endpoint.SecurityGroupIDs) != 1 || endpoint.SecurityGroupIDs[0] != "sg-1" {
		t.Fatalf("security groups = %#v", endpoint.SecurityGroupIDs)
	}
	if len(endpoint.DNSEntries) != 1 {
		t.Fatalf("dns entries = %#v", endpoint.DNSEntries)
	}
}

func TestMapVPNConnectionDoesNotExposeTunnelPSK(t *testing.T) {
	// The SDK VpnConnection struct carries CustomerGatewayConfiguration (an
	// XML body) that can contain tunnel pre-shared keys. The scanner-owned
	// VPNConnection type intentionally omits that field. This test fails if a
	// future refactor adds a PreSharedKey or CustomerGatewayConfiguration field
	// to the mapped struct.
	mapped := mapVPNConnection(awsec2types.VpnConnection{
		VpnConnectionId:   aws.String("vpn-1"),
		State:             awsec2types.VpnStateAvailable,
		Type:              awsec2types.GatewayTypeIpsec1,
		CustomerGatewayId: aws.String("cgw-1"),
		VpnGatewayId:      aws.String("vgw-1"),
		// CustomerGatewayConfiguration intentionally not mapped.
	})
	value := reflect.ValueOf(mapped)
	mappedType := value.Type()
	forbiddenFields := []string{"PreSharedKey", "CustomerGatewayConfiguration"}
	for _, field := range forbiddenFields {
		if _, ok := mappedType.FieldByName(field); ok {
			t.Fatalf("VPNConnection mapped type exposes %q; tunnel secrets must never be persisted", field)
		}
	}
	if mapped.ID != "vpn-1" {
		t.Fatalf("ID = %q", mapped.ID)
	}
}

func TestMapElasticIPPreservesAllocationAndAssociation(t *testing.T) {
	eip := mapElasticIP(awsec2types.Address{
		AllocationId:            aws.String("eipalloc-1"),
		AssociationId:           aws.String("eipassoc-1"),
		Domain:                  awsec2types.DomainTypeVpc,
		PublicIp:                aws.String("203.0.113.10"),
		NetworkBorderGroup:      aws.String("us-east-1"),
		InstanceId:              aws.String("i-1234567890abcdef0"),
		NetworkInterfaceId:      aws.String("eni-1"),
		NetworkInterfaceOwnerId: aws.String("123456789012"),
		PrivateIpAddress:        aws.String("10.0.1.5"),
		Tags:                    []awsec2types.Tag{{Key: aws.String("Owner"), Value: aws.String("platform")}},
	})
	if eip.AllocationID != "eipalloc-1" {
		t.Fatalf("allocation id = %q", eip.AllocationID)
	}
	if eip.Domain != "vpc" {
		t.Fatalf("domain = %q", eip.Domain)
	}
	if eip.InstanceID != "i-1234567890abcdef0" {
		t.Fatalf("instance id = %q", eip.InstanceID)
	}
}

func TestMapDHCPOptionsConcatenatesValues(t *testing.T) {
	options := mapDHCPOptions(awsec2types.DhcpOptions{
		DhcpOptionsId: aws.String("dopt-1"),
		OwnerId:       aws.String("123456789012"),
		DhcpConfigurations: []awsec2types.DhcpConfiguration{{
			Key: aws.String("domain-name-servers"),
			Values: []awsec2types.AttributeValue{
				{Value: aws.String("10.0.0.2")},
				{Value: aws.String("AmazonProvidedDNS")},
			},
		}},
	})
	if options.ID != "dopt-1" {
		t.Fatalf("id = %q", options.ID)
	}
	if len(options.Configuration) != 1 || len(options.Configuration[0].Values) != 2 {
		t.Fatalf("configuration = %#v", options.Configuration)
	}
}
