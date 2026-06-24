package vpc

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestScannerEmitsRouteTableAndRelationships(t *testing.T) {
	envelopes := scanFixture(t, fakeClient{
		routeTables: []RouteTable{{
			ID:      "rtb-1",
			VPCID:   "vpc-1",
			OwnerID: "123456789012",
			Associations: []RouteTableAssociation{
				{AssociationID: "rtbassoc-1", SubnetID: "subnet-1", Main: false, State: "associated"},
				{AssociationID: "rtbassoc-main", Main: true, State: "associated"},
			},
			Routes: []Route{
				{DestinationCIDRBlock: "0.0.0.0/0", GatewayID: "igw-1", State: "active", Origin: "CreateRoute"},
				{DestinationCIDRBlock: "10.1.0.0/16", NATGatewayID: "nat-1", State: "active"},
				{DestinationPrefixListID: "pl-1", VPCEndpointID: "vpce-1", State: "active"},
				{DestinationCIDRBlock: "10.2.0.0/16", VPCPeeringConnectionID: "pcx-1"},
				{DestinationCIDRBlock: "10.3.0.0/16", TransitGatewayID: "tgw-1"},
				{DestinationCIDRBlock: "10.0.0.0/16", GatewayID: "local", Origin: "CreateRouteTable"},
			},
			Tags: map[string]string{"Name": "primary-rtb"},
		}},
	})

	rt := resourceByType(t, envelopes, awscloud.ResourceTypeVPCRouteTable)
	attributes := attributesOf(t, rt)
	if got, want := attributes["vpc_id"], "vpc-1"; got != want {
		t.Fatalf("route table vpc_id = %#v, want %q", got, want)
	}

	assertRelationship(t, envelopes, awscloud.RelationshipVPCRouteTableInVPC)
	assertRelationship(t, envelopes, awscloud.RelationshipVPCRouteTableAssociatedWithSubnet)
	assertRelationship(t, envelopes, awscloud.RelationshipVPCRouteTargetsInternetGateway)
	assertRelationship(t, envelopes, awscloud.RelationshipVPCRouteTargetsNATGateway)
	assertRelationship(t, envelopes, awscloud.RelationshipVPCRouteTargetsVPCEndpoint)
	assertRelationship(t, envelopes, awscloud.RelationshipVPCRouteTargetsPeeringConnection)
	assertRelationship(t, envelopes, awscloud.RelationshipVPCRouteTargetsTransitGateway)

	// Local route (gateway_id="local") MUST NOT generate an internet-gateway
	// relationship. Only igw-prefixed gateway IDs are internet gateways.
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if envelope.Payload["relationship_type"] == awscloud.RelationshipVPCRouteTargetsInternetGateway {
			if target, _ := envelope.Payload["target_resource_id"].(string); target == "local" {
				t.Fatalf("local route emitted as internet-gateway edge: %#v", envelope.Payload)
			}
		}
	}
}

func TestScannerEmitsInternetGatewayWithVPCAttachment(t *testing.T) {
	envelopes := scanFixture(t, fakeClient{
		internetGateways: []InternetGateway{{
			ID:      "igw-1",
			OwnerID: "123456789012",
			Attachments: []InternetGatewayAttachment{
				{VPCID: "vpc-1", State: "available"},
			},
			Tags: map[string]string{"Name": "primary-igw"},
		}},
	})

	igw := resourceByType(t, envelopes, awscloud.ResourceTypeVPCInternetGateway)
	if got, want := igw.Payload["resource_id"], "igw-1"; got != want {
		t.Fatalf("igw resource_id = %#v, want %q", got, want)
	}
	rel := relationshipByType(t, envelopes, awscloud.RelationshipVPCInternetGatewayAttachedToVPC)
	if got, want := rel.Payload["target_resource_id"], "vpc-1"; got != want {
		t.Fatalf("igw attachment target = %#v, want %q", got, want)
	}
	if got, want := rel.Payload["target_type"], awscloud.ResourceTypeEC2VPC; got != want {
		t.Fatalf("igw attachment target_type = %#v, want %q (EC2-owned)", got, want)
	}
}

func TestScannerEmitsNATGatewaySubnetAndVPCEdges(t *testing.T) {
	envelopes := scanFixture(t, fakeClient{
		natGateways: []NATGateway{{
			ID:               "nat-1",
			VPCID:            "vpc-1",
			SubnetID:         "subnet-1",
			State:            "available",
			ConnectivityType: "public",
			NATGatewayAddresses: []NATGatewayAddress{{
				AllocationID:       "eipalloc-1",
				NetworkInterfaceID: "eni-1",
				PrivateIP:          "10.0.1.5",
				PublicIP:           "203.0.113.10",
				IsPrimary:          true,
			}},
			Tags: map[string]string{"Name": "primary-nat"},
		}},
	})

	natResource := resourceByType(t, envelopes, awscloud.ResourceTypeVPCNATGateway)
	attributes := attributesOf(t, natResource)
	if got, want := attributes["subnet_id"], "subnet-1"; got != want {
		t.Fatalf("nat subnet_id = %#v, want %q", got, want)
	}
	assertRelationship(t, envelopes, awscloud.RelationshipVPCNATGatewayInSubnet)
	assertRelationship(t, envelopes, awscloud.RelationshipVPCNATGatewayInVPC)
}

func TestScannerEmitsNetworkACLAndSubnetAssociations(t *testing.T) {
	envelopes := scanFixture(t, fakeClient{
		networkACLs: []NetworkACL{{
			ID:        "acl-1",
			VPCID:     "vpc-1",
			OwnerID:   "123456789012",
			IsDefault: false,
			Associations: []NetworkACLAssociation{
				{AssociationID: "aclassoc-1", SubnetID: "subnet-1"},
				{AssociationID: "aclassoc-2", SubnetID: "subnet-2"},
			},
			Entries: []NetworkACLEntry{{
				RuleNumber: 100,
				Protocol:   "6",
				RuleAction: "allow",
				Egress:     false,
				CIDRBlock:  "0.0.0.0/0",
			}},
			Tags: map[string]string{"Name": "primary-acl"},
		}},
	})

	assertRelationship(t, envelopes, awscloud.RelationshipVPCNetworkACLInVPC)
	assertRelationship(t, envelopes, awscloud.RelationshipVPCNetworkACLAssociatedWithSubnet)
	resource := resourceByType(t, envelopes, awscloud.ResourceTypeVPCNetworkACL)
	attributes := attributesOf(t, resource)
	entries, ok := attributes["entries"].([]map[string]any)
	if !ok || len(entries) != 1 {
		t.Fatalf("entries = %#v, want one entry", attributes["entries"])
	}
	if got, want := entries[0]["rule_number"], int32(100); got != want {
		t.Fatalf("entries[0].rule_number = %#v, want %v", got, want)
	}
}

func TestScannerEmitsVPCPeeringBothSides(t *testing.T) {
	envelopes := scanFixture(t, fakeClient{
		peerings: []VPCPeeringConnection{{
			ID:     "pcx-1",
			Status: "active",
			Requester: VPCPeeringVPCInfo{
				VPCID: "vpc-1", OwnerID: "123456789012", Region: "us-east-1", CIDRBlock: "10.0.0.0/16",
			},
			Accepter: VPCPeeringVPCInfo{
				VPCID: "vpc-2", OwnerID: "210987654321", Region: "us-west-2", CIDRBlock: "10.1.0.0/16",
			},
			Tags: map[string]string{"Purpose": "peering"},
		}},
	})

	var requesterSeen, accepterSeen bool
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if envelope.Payload["relationship_type"] != awscloud.RelationshipVPCPeeringConnectsVPC {
			continue
		}
		attributes, _ := envelope.Payload["attributes"].(map[string]any)
		side, _ := attributes["side"].(string)
		target, _ := envelope.Payload["target_resource_id"].(string)
		switch side {
		case "requester":
			if target != "vpc-1" {
				t.Fatalf("requester edge target = %q, want vpc-1", target)
			}
			requesterSeen = true
		case "accepter":
			if target != "vpc-2" {
				t.Fatalf("accepter edge target = %q, want vpc-2", target)
			}
			accepterSeen = true
		}
	}
	if !requesterSeen || !accepterSeen {
		t.Fatalf("peering edges missing requester=%v accepter=%v", requesterSeen, accepterSeen)
	}
}

func TestScannerEmitsVPCEndpointVPCAndServiceEdges(t *testing.T) {
	envelopes := scanFixture(t, fakeClient{
		endpoints: []VPCEndpoint{{
			ID:                "vpce-1",
			VPCID:             "vpc-1",
			ServiceName:       "com.amazonaws.us-east-1.s3",
			EndpointType:      "Gateway",
			State:             "available",
			PrivateDNSEnabled: false,
			OwnerID:           "123456789012",
			RouteTableIDs:     []string{"rtb-1"},
			Tags:              map[string]string{"Name": "s3-endpoint"},
		}, {
			ID:                "vpce-2",
			VPCID:             "vpc-1",
			ServiceName:       "com.amazonaws.us-east-1.execute-api",
			EndpointType:      "Interface",
			State:             "available",
			PrivateDNSEnabled: true,
			SubnetIDs:         []string{"subnet-1"},
			SecurityGroupIDs:  []string{"sg-1"},
		}},
	})

	gatewayEndpoint := resourceByID(t, envelopes, awscloud.ResourceTypeVPCEndpoint, "vpce-1")
	if got := attributesOf(t, gatewayEndpoint)["endpoint_type"]; got != "Gateway" {
		t.Fatalf("gateway endpoint endpoint_type = %#v", got)
	}
	interfaceEndpoint := resourceByID(t, envelopes, awscloud.ResourceTypeVPCEndpoint, "vpce-2")
	if got := attributesOf(t, interfaceEndpoint)["endpoint_type"]; got != "Interface" {
		t.Fatalf("interface endpoint endpoint_type = %#v", got)
	}
	assertRelationship(t, envelopes, awscloud.RelationshipVPCEndpointInVPC)
	assertRelationship(t, envelopes, awscloud.RelationshipVPCEndpointUsesService)
}

func TestScannerEmitsElasticIPInstanceAndENIEdges(t *testing.T) {
	envelopes := scanFixture(t, fakeClient{
		elasticIPs: []ElasticIP{{
			AllocationID:            "eipalloc-1",
			AssociationID:           "eipassoc-1",
			Domain:                  "vpc",
			PublicIP:                "203.0.113.10",
			InstanceID:              "i-1234567890abcdef0",
			NetworkInterfaceID:      "eni-1",
			NetworkInterfaceOwnerID: "123456789012",
			PrivateIP:               "10.0.1.5",
			Tags:                    map[string]string{"Owner": "platform"},
		}},
	})

	resource := resourceByType(t, envelopes, awscloud.ResourceTypeVPCElasticIP)
	if got, want := resource.Payload["resource_id"], "eipalloc-1"; got != want {
		t.Fatalf("elastic IP resource_id = %#v, want %q", got, want)
	}
	instanceRel := relationshipByType(t, envelopes, awscloud.RelationshipVPCElasticIPAssociatedWithInstance)
	if got, want := instanceRel.Payload["target_type"], "aws_ec2_instance"; got != want {
		t.Fatalf("EIP->instance target_type = %#v, want %q", got, want)
	}
	eniRel := relationshipByType(t, envelopes, awscloud.RelationshipVPCElasticIPAssociatedWithNetworkInterface)
	if got, want := eniRel.Payload["target_type"], awscloud.ResourceTypeEC2NetworkInterface; got != want {
		t.Fatalf("EIP->ENI target_type = %#v, want %q (EC2-owned)", got, want)
	}
}

func TestScannerEmitsDHCPOptionsAndCustomerGatewayResources(t *testing.T) {
	envelopes := scanFixture(t, fakeClient{
		dhcpOptions: []DHCPOptions{{
			ID:      "dopt-1",
			OwnerID: "123456789012",
			Configuration: []DHCPConfigurationEntry{
				{Key: "domain-name", Values: []string{"example.internal"}},
				{Key: "domain-name-servers", Values: []string{"AmazonProvidedDNS"}},
			},
			Tags: map[string]string{"Name": "default"},
		}},
		customerGateways: []CustomerGateway{{
			ID:        "cgw-1",
			State:     "available",
			Type:      "ipsec.1",
			IPAddress: "203.0.113.50",
			BGPASN:    "65000",
			Tags:      map[string]string{"Site": "office"},
		}},
	})

	dhcp := resourceByType(t, envelopes, awscloud.ResourceTypeVPCDHCPOptions)
	if got, want := dhcp.Payload["resource_id"], "dopt-1"; got != want {
		t.Fatalf("dhcp options resource_id = %#v, want %q", got, want)
	}
	cgw := resourceByType(t, envelopes, awscloud.ResourceTypeVPCCustomerGateway)
	if got, want := cgw.Payload["resource_id"], "cgw-1"; got != want {
		t.Fatalf("customer gateway resource_id = %#v, want %q", got, want)
	}
}

func TestScannerEmitsVPNGatewayAndConnectionEdges(t *testing.T) {
	envelopes := scanFixture(t, fakeClient{
		vpnGateways: []VPNGateway{{
			ID:             "vgw-1",
			State:          "available",
			Type:           "ipsec.1",
			AmazonSideASN:  64512,
			VPCAttachments: []VPNGatewayAttachment{{VPCID: "vpc-1", State: "attached"}},
			Tags:           map[string]string{"Name": "primary-vgw"},
		}},
		vpnConnections: []VPNConnection{{
			ID:                "vpn-1",
			State:             "available",
			Type:              "ipsec.1",
			Category:          "VPN",
			CustomerGatewayID: "cgw-1",
			VPNGatewayID:      "vgw-1",
			StaticRoutesOnly:  true,
			TelemetrySummaries: []VPNTunnelTelemetry{{
				OutsideIPAddress:   "203.0.113.50",
				Status:             "UP",
				StatusMessage:      "IPSEC IS UP",
				AcceptedRouteCount: 1,
				LastStatusChange:   time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC),
			}},
			Tags: map[string]string{"Name": "primary-vpn"},
		}, {
			ID:                "vpn-tgw",
			State:             "available",
			Type:              "ipsec.1",
			CustomerGatewayID: "cgw-1",
			TransitGatewayID:  "tgw-1",
		}},
	})

	assertRelationship(t, envelopes, awscloud.RelationshipVPCVPNGatewayAttachedToVPC)
	assertRelationship(t, envelopes, awscloud.RelationshipVPCVPNConnectionUsesCustomerGateway)
	assertRelationship(t, envelopes, awscloud.RelationshipVPCVPNConnectionUsesVPNGateway)
	assertRelationship(t, envelopes, awscloud.RelationshipVPCVPNConnectionUsesTransitGateway)

	connection := resourceByID(t, envelopes, awscloud.ResourceTypeVPCVPNConnection, "vpn-1")
	attributes := attributesOf(t, connection)
	telemetry, ok := attributes["telemetry"].([]map[string]any)
	if !ok || len(telemetry) != 1 {
		t.Fatalf("telemetry = %#v, want one summary", attributes["telemetry"])
	}
	if _, exists := telemetry[0]["pre_shared_key"]; exists {
		t.Fatalf("telemetry persisted a pre-shared key; the VPN scanner must never store PSK material")
	}
	if _, exists := attributes["pre_shared_key"]; exists {
		t.Fatalf("VPN connection persisted a pre-shared key field")
	}
}

func TestScannerRejectsMismatchedServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = awscloud.ServiceEC2

	_, err := (Scanner{Client: fakeClient{}}).Scan(context.Background(), boundary)
	if err == nil {
		t.Fatalf("Scan() error = nil, want service kind mismatch")
	}
	if !strings.Contains(err.Error(), "service_kind") {
		t.Fatalf("Scan() error = %v, want service_kind mismatch", err)
	}
}

func TestScannerRequiresClient(t *testing.T) {
	_, err := (Scanner{}).Scan(context.Background(), testBoundary())
	if err == nil {
		t.Fatalf("Scan() error = nil, want client required")
	}
}

func TestScannerPropagatesClientErrors(t *testing.T) {
	listErr := errors.New("describe failed")
	_, err := (Scanner{Client: fakeClient{routeTablesErr: listErr}}).Scan(context.Background(), testBoundary())
	if !errors.Is(err, listErr) {
		t.Fatalf("Scan() error = %v, want wrapping listErr", err)
	}
}

func TestVPCResourceTypesDisjointFromEC2(t *testing.T) {
	// The VPC scanner package is the network-fabric overlay; the EC2 scanner
	// owns instance/ENI surface. The two scanners MUST NOT emit the same
	// resource_type. This test pins the boundary so a regression cannot be
	// merged silently.
	ec2Owned := map[string]struct{}{
		awscloud.ResourceTypeEC2VPC:               {},
		awscloud.ResourceTypeEC2Subnet:            {},
		awscloud.ResourceTypeEC2SecurityGroup:     {},
		awscloud.ResourceTypeEC2SecurityGroupRule: {},
		awscloud.ResourceTypeEC2NetworkInterface:  {},
	}
	vpcEmitted := []string{
		awscloud.ResourceTypeVPCRouteTable,
		awscloud.ResourceTypeVPCInternetGateway,
		awscloud.ResourceTypeVPCNATGateway,
		awscloud.ResourceTypeVPCNetworkACL,
		awscloud.ResourceTypeVPCPeeringConnection,
		awscloud.ResourceTypeVPCEndpoint,
		awscloud.ResourceTypeVPCElasticIP,
		awscloud.ResourceTypeVPCDHCPOptions,
		awscloud.ResourceTypeVPCCustomerGateway,
		awscloud.ResourceTypeVPCVPNGateway,
		awscloud.ResourceTypeVPCVPNConnection,
	}
	seen := map[string]struct{}{}
	for _, resourceType := range vpcEmitted {
		if _, owned := ec2Owned[resourceType]; owned {
			t.Fatalf("VPC scanner claims EC2-owned resource_type %q", resourceType)
		}
		if _, duplicate := seen[resourceType]; duplicate {
			t.Fatalf("VPC scanner declares duplicate resource_type %q", resourceType)
		}
		seen[resourceType] = struct{}{}
	}
}

func TestClientInterfaceIsReadOnly(t *testing.T) {
	clientType := reflect.TypeOf((*Client)(nil)).Elem()
	forbiddenPrefixes := []string{
		"Create",
		"Delete",
		"Modify",
		"Allocate",
		"Release",
		"Associate",
		"Disassociate",
		"Attach",
		"Detach",
		"Accept",
		"Reject",
		"Replace",
		"Authorize",
		"Revoke",
		"Update",
		"Enable",
		"Disable",
		"Restore",
		"Reset",
	}
	for i := 0; i < clientType.NumMethod(); i++ {
		method := clientType.Method(i)
		for _, prefix := range forbiddenPrefixes {
			if strings.HasPrefix(method.Name, prefix) {
				t.Fatalf("Client method %q has forbidden prefix %q; vpc scanner Client must be read-only", method.Name, prefix)
			}
		}
		if !strings.HasPrefix(method.Name, "List") {
			t.Fatalf("Client method %q is not a List* read; vpc scanner Client must only expose list operations", method.Name)
		}
	}
}
