// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package transitgateway

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

func TestScannerRequiresClient(t *testing.T) {
	_, err := (Scanner{}).Scan(context.Background(), testBoundary())
	if err == nil {
		t.Fatal("Scan() error = nil, want error for nil client")
	}
}

func TestScannerRejectsForeignServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = awscloud.ServiceVPC
	_, err := (Scanner{Client: fakeClient{}}).Scan(context.Background(), boundary)
	if err == nil {
		t.Fatal("Scan() error = nil, want error for foreign service_kind")
	}
}

func TestScannerPropagatesListError(t *testing.T) {
	_, err := (Scanner{Client: fakeClient{transitGatewaysErr: errors.New("boom")}}).
		Scan(context.Background(), testBoundary())
	if err == nil {
		t.Fatal("Scan() error = nil, want propagated list error")
	}
}

func TestScannerEmitsTransitGateway(t *testing.T) {
	envelopes := scanFixture(t, fakeClient{
		transitGateways: []TransitGateway{{
			ID:          "tgw-1",
			ARN:         "arn:aws:ec2:us-east-1:123456789012:transit-gateway/tgw-1",
			OwnerID:     "123456789012",
			State:       "available",
			Description: "core hub",
			CreatedAt:   time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
			Options: TransitGatewayOptions{
				AmazonSideASN:                  64512,
				AssociationDefaultRouteTableID: "tgw-rtb-assoc",
				PropagationDefaultRouteTableID: "tgw-rtb-prop",
				DefaultRouteTableAssociation:   "enable",
				DNSSupport:                     "enable",
				MulticastSupport:               "enable",
			},
			Tags: map[string]string{"Name": "core-tgw"},
		}},
	})

	tgw := resourceByType(t, envelopes, awscloud.ResourceTypeTransitGateway)
	if got, _ := tgw.Payload["resource_id"].(string); got != "tgw-1" {
		t.Fatalf("transit gateway resource_id = %q", got)
	}
	if got, _ := tgw.Payload["arn"].(string); got != "arn:aws:ec2:us-east-1:123456789012:transit-gateway/tgw-1" {
		t.Fatalf("transit gateway arn = %q", got)
	}
	attributes := attributesOf(t, tgw)
	options, ok := attributes["options"].(map[string]any)
	if !ok {
		t.Fatalf("options = %#v, want map", attributes["options"])
	}
	if got := options["amazon_side_asn"]; got != int64(64512) {
		t.Fatalf("amazon_side_asn = %#v, want 64512", got)
	}
}

func TestScannerEmitsRouteTableInTransitGateway(t *testing.T) {
	envelopes := scanFixture(t, fakeClient{
		routeTables: []RouteTable{{
			ID:                           "tgw-rtb-1",
			TransitGatewayID:             "tgw-1",
			State:                        "available",
			DefaultAssociationRouteTable: true,
		}},
	})

	rt := resourceByType(t, envelopes, awscloud.ResourceTypeTransitGatewayRouteTable)
	if got, _ := rt.Payload["resource_id"].(string); got != "tgw-rtb-1" {
		t.Fatalf("route table resource_id = %q", got)
	}
	edge := relationshipByType(t, envelopes, awscloud.RelationshipTransitGatewayRouteTableInTransitGateway)
	if got, _ := edge.Payload["target_resource_id"].(string); got != "tgw-1" {
		t.Fatalf("route table -> tgw target = %q", got)
	}
}

func TestScannerEmitsVPCAttachmentRelationshipsAndRouteTableAssociation(t *testing.T) {
	envelopes := scanFixture(t, fakeClient{
		attachments: []Attachment{{
			ID:                      "tgw-attach-vpc",
			TransitGatewayID:        "tgw-1",
			TransitGatewayOwnerID:   "123456789012",
			ResourceType:            "vpc",
			ResourceID:              "vpc-1",
			ResourceOwnerID:         "123456789012",
			State:                   "available",
			AssociationRouteTableID: "tgw-rtb-1",
			AssociationState:        "associated",
		}},
	})

	resourceByType(t, envelopes, awscloud.ResourceTypeTransitGatewayAttachment)
	toTGW := relationshipByType(t, envelopes, awscloud.RelationshipTransitGatewayAttachmentToTransitGateway)
	if got, _ := toTGW.Payload["target_resource_id"].(string); got != "tgw-1" {
		t.Fatalf("attachment -> tgw target = %q", got)
	}
	toVPC := relationshipByType(t, envelopes, awscloud.RelationshipTransitGatewayAttachmentToVPC)
	if got, _ := toVPC.Payload["target_type"].(string); got != awscloud.ResourceTypeEC2VPC {
		t.Fatalf("attachment -> vpc target_type = %q", got)
	}
	rtToAttachment := relationshipByType(t, envelopes, awscloud.RelationshipTransitGatewayRouteTableToAttachment)
	if got, _ := rtToAttachment.Payload["source_resource_id"].(string); got != "tgw-rtb-1" {
		t.Fatalf("route-table -> attachment source = %q", got)
	}
	if got, _ := rtToAttachment.Payload["target_resource_id"].(string); got != "tgw-attach-vpc" {
		t.Fatalf("route-table -> attachment target = %q", got)
	}
}

func TestScannerEmitsVPNAndDirectConnectAttachments(t *testing.T) {
	envelopes := scanFixture(t, fakeClient{
		attachments: []Attachment{
			{ID: "tgw-attach-vpn", TransitGatewayID: "tgw-1", ResourceType: "vpn", ResourceID: "vpn-1", State: "available"},
			{ID: "tgw-attach-dx", TransitGatewayID: "tgw-1", ResourceType: "direct-connect-gateway", ResourceID: "dxgw-1", State: "available"},
		},
	})

	vpn := relationshipByType(t, envelopes, awscloud.RelationshipTransitGatewayAttachmentToVPNConnection)
	if got, _ := vpn.Payload["target_type"].(string); got != awscloud.ResourceTypeVPCVPNConnection {
		t.Fatalf("vpn attachment target_type = %q", got)
	}
	dx := relationshipByType(t, envelopes, awscloud.RelationshipTransitGatewayAttachmentToDirectConnectGateway)
	if got, _ := dx.Payload["target_resource_id"].(string); got != "dxgw-1" {
		t.Fatalf("dx attachment target = %q", got)
	}
}

// TestScannerDoesNotEmitResourceEdgeForUnknownAttachmentType is the negative
// case: a Connect attachment (or any type without a typed target) emits the
// attachment resource and the attachment->tgw edge but no fabricated
// resource edge.
func TestScannerDoesNotEmitResourceEdgeForUnknownAttachmentType(t *testing.T) {
	envelopes := scanFixture(t, fakeClient{
		attachments: []Attachment{{
			ID:               "tgw-attach-connect",
			TransitGatewayID: "tgw-1",
			ResourceType:     "connect",
			ResourceID:       "tgw-attach-vpc",
			State:            "available",
		}},
	})

	resourceByType(t, envelopes, awscloud.ResourceTypeTransitGatewayAttachment)
	if !hasRelationship(envelopes, awscloud.RelationshipTransitGatewayAttachmentToTransitGateway) {
		t.Fatal("connect attachment must still emit attachment -> tgw edge")
	}
	for _, forbidden := range []string{
		awscloud.RelationshipTransitGatewayAttachmentToVPC,
		awscloud.RelationshipTransitGatewayAttachmentToVPNConnection,
		awscloud.RelationshipTransitGatewayAttachmentToDirectConnectGateway,
		awscloud.RelationshipTransitGatewayAttachmentToPeer,
	} {
		if hasRelationship(envelopes, forbidden) {
			t.Fatalf("connect attachment fabricated resource edge %q", forbidden)
		}
	}
}

// TestScannerSurfacesCrossAccountPeerWithoutResolving is the ambiguous /
// security-review case: a peering attachment whose accepter is a transit
// gateway in a different account must be emitted with the remote account
// identity as AWS reports it and flagged cross_account, without the scanner
// resolving the remote account.
func TestScannerSurfacesCrossAccountPeerWithoutResolving(t *testing.T) {
	envelopes := scanFixture(t, fakeClient{
		peeringAttachments: []PeeringAttachment{{
			ID:         "tgw-attach-peer",
			State:      "available",
			StatusCode: "available",
			Requester: PeeringTransitGatewayInfo{
				TransitGatewayID: "tgw-1",
				OwnerID:          "123456789012",
				Region:           "us-east-1",
			},
			Accepter: PeeringTransitGatewayInfo{
				TransitGatewayID: "tgw-remote",
				OwnerID:          "999988887777",
				Region:           "eu-west-1",
			},
		}},
	})

	resourceByType(t, envelopes, awscloud.ResourceTypeTransitGatewayPeeringAttachment)

	requester := relationshipByType(t, envelopes, awscloud.RelationshipTransitGatewayPeeringRequestsTransitGateway)
	requesterAttrs, _ := requester.Payload["attributes"].(map[string]any)
	if got := requesterAttrs["cross_account"]; got != false {
		t.Fatalf("requester cross_account = %#v, want false (same account)", got)
	}

	accepter := relationshipByType(t, envelopes, awscloud.RelationshipTransitGatewayPeeringAcceptsTransitGateway)
	if got, _ := accepter.Payload["target_resource_id"].(string); got != "tgw-remote" {
		t.Fatalf("accepter target = %q, want remote tgw as reported", got)
	}
	accepterAttrs, _ := accepter.Payload["attributes"].(map[string]any)
	if got := accepterAttrs["cross_account"]; got != true {
		t.Fatalf("accepter cross_account = %#v, want true", got)
	}
	if got := accepterAttrs["owner_id"]; got != "999988887777" {
		t.Fatalf("accepter owner_id = %#v, want the remote account surfaced as-is", got)
	}
	// The scanner must not invent an aws_account node or resolve the remote
	// identity; the relationship target stays the bare remote transit gateway.
	if got, _ := accepter.Payload["target_type"].(string); got != awscloud.ResourceTypeTransitGateway {
		t.Fatalf("accepter target_type = %q, want bare transit gateway", got)
	}
}

func TestScannerEmitsMulticastDomainAndPolicyTable(t *testing.T) {
	envelopes := scanFixture(t, fakeClient{
		multicastDomains: []MulticastDomain{{
			ID:               "tgw-mcast-1",
			ARN:              "arn:aws:ec2:us-east-1:123456789012:transit-gateway-multicast-domain/tgw-mcast-1",
			TransitGatewayID: "tgw-1",
			State:            "available",
		}},
		policyTables: []PolicyTable{{
			ID:               "tgw-pt-1",
			TransitGatewayID: "tgw-1",
			State:            "available",
		}},
	})

	resourceByType(t, envelopes, awscloud.ResourceTypeTransitGatewayMulticastDomain)
	resourceByType(t, envelopes, awscloud.ResourceTypeTransitGatewayPolicyTable)
	relationshipByType(t, envelopes, awscloud.RelationshipTransitGatewayMulticastDomainInTransitGateway)
	relationshipByType(t, envelopes, awscloud.RelationshipTransitGatewayPolicyTableInTransitGateway)
}

// TestClientInterfaceIsReadOnly pins the scanner-owned Client contract to
// read-only methods. The issue forbids Create/Delete/Modify of transit
// gateways, attachments, route tables, and multicast domains, plus
// AssociateTransitGatewayRouteTable and EnableTransitGatewayRouteTablePropagation.
// Any method whose name begins with a mutation verb fails this test before the
// scanner can be compiled.
func TestClientInterfaceIsReadOnly(t *testing.T) {
	clientType := reflect.TypeOf((*Client)(nil)).Elem()
	forbiddenPrefixes := []string{
		"Create", "Delete", "Modify", "Associate", "Disassociate",
		"Accept", "Reject", "Enable", "Disable", "Replace", "Register",
		"Deregister", "Update", "Apply", "Restore", "Reset",
	}
	for i := 0; i < clientType.NumMethod(); i++ {
		method := clientType.Method(i)
		for _, prefix := range forbiddenPrefixes {
			if strings.HasPrefix(method.Name, prefix) {
				t.Fatalf("Client method %q has forbidden prefix %q; transit gateway Client must be read-only", method.Name, prefix)
			}
		}
		if !strings.HasPrefix(method.Name, "List") {
			t.Fatalf("Client method %q is not a List read", method.Name)
		}
	}
}

// TestResourceTypesDisjointFromVPC pins the boundary with the VPC scanner. The
// VPC scanner owns aws_vpc_route_table and aws_vpc_vpn_connection; this scanner
// owns the transit-gateway-prefixed types. They must not collide, otherwise the
// two scanners would claim the same node.
func TestResourceTypesDisjointFromVPC(t *testing.T) {
	vpcOwned := map[string]struct{}{
		awscloud.ResourceTypeVPCRouteTable:    {},
		awscloud.ResourceTypeVPCVPNConnection: {},
	}
	emitted := []string{
		awscloud.ResourceTypeTransitGateway,
		awscloud.ResourceTypeTransitGatewayRouteTable,
		awscloud.ResourceTypeTransitGatewayAttachment,
		awscloud.ResourceTypeTransitGatewayPeeringAttachment,
		awscloud.ResourceTypeTransitGatewayMulticastDomain,
		awscloud.ResourceTypeTransitGatewayPolicyTable,
	}
	seen := map[string]struct{}{}
	for _, resourceType := range emitted {
		if _, owned := vpcOwned[resourceType]; owned {
			t.Fatalf("transit gateway scanner claims VPC-owned resource_type %q", resourceType)
		}
		if _, dup := seen[resourceType]; dup {
			t.Fatalf("duplicate emitted resource_type %q", resourceType)
		}
		seen[resourceType] = struct{}{}
	}
}
