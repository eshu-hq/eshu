// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package directconnect

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

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
	_, err := (Scanner{Client: fakeClient{connectionsErr: errors.New("boom")}}).
		Scan(context.Background(), testBoundary())
	if err == nil {
		t.Fatal("Scan() error = nil, want propagated list error")
	}
}

func TestScannerEmitsConnection(t *testing.T) {
	envelopes := scanFixture(t, fakeClient{
		connections: []Connection{{
			ID:            "dxcon-1",
			Name:          "primary-port",
			OwnerAccount:  "123456789012",
			Location:      "EqDC2",
			Bandwidth:     "10Gbps",
			State:         "available",
			Region:        "us-east-1",
			PartnerName:   "Example Partner",
			ProviderName:  "Example Provider",
			VLAN:          0,
			MacSecCapable: true,
			Tags:          map[string]string{"Name": "primary-port"},
		}},
	})

	conn := resourceByType(t, envelopes, awscloud.ResourceTypeDirectConnectConnection)
	if got, _ := conn.Payload["resource_id"].(string); got != "dxcon-1" {
		t.Fatalf("connection resource_id = %q", got)
	}
	if got, _ := conn.Payload["state"].(string); got != "available" {
		t.Fatalf("connection state = %q", got)
	}
	attributes := attributesOf(t, conn)
	if got := attributes["location"]; got != "EqDC2" {
		t.Fatalf("connection location = %#v", got)
	}
	if got := attributes["bandwidth"]; got != "10Gbps" {
		t.Fatalf("connection bandwidth = %#v", got)
	}
	if got := attributes["partner_name"]; got != "Example Partner" {
		t.Fatalf("connection partner_name = %#v", got)
	}
}

func TestScannerEmitsConnectionInLAG(t *testing.T) {
	envelopes := scanFixture(t, fakeClient{
		connections: []Connection{{
			ID:    "dxcon-1",
			Name:  "member-port",
			State: "available",
			LAGID: "dxlag-1",
		}},
		lags: []LAG{{
			ID:        "dxlag-1",
			Name:      "bundle",
			State:     "available",
			Bandwidth: "10Gbps",
			Location:  "EqDC2",
		}},
	})

	resourceByType(t, envelopes, awscloud.ResourceTypeDirectConnectLAG)
	edge := relationshipByType(t, envelopes, awscloud.RelationshipDirectConnectConnectionInLAG)
	if got, _ := edge.Payload["source_resource_id"].(string); got != "dxcon-1" {
		t.Fatalf("connection->lag source = %q", got)
	}
	if got, _ := edge.Payload["target_resource_id"].(string); got != "dxlag-1" {
		t.Fatalf("connection->lag target = %q", got)
	}
	if got, _ := edge.Payload["target_type"].(string); got != awscloud.ResourceTypeDirectConnectLAG {
		t.Fatalf("connection->lag target_type = %q", got)
	}
}

func TestScannerEmitsVirtualInterfaceEdges(t *testing.T) {
	envelopes := scanFixture(t, fakeClient{
		virtualInterfaces: []VirtualInterface{{
			ID:           "dxvif-1",
			Name:         "prod-vif",
			Type:         "transit",
			State:        "available",
			ConnectionID: "dxcon-1",
			GatewayID:    "dxgw-1",
			VLAN:         101,
			ASN:          65000,
		}},
	})

	vif := resourceByType(t, envelopes, awscloud.ResourceTypeDirectConnectVirtualInterface)
	attributes := attributesOf(t, vif)
	if got := attributes["virtual_interface_type"]; got != "transit" {
		t.Fatalf("virtual_interface_type = %#v", got)
	}
	if got := attributes["vlan"]; got != int32(101) {
		t.Fatalf("vlan = %#v, want 101", got)
	}
	if got := attributes["bgp_asn"]; got != int32(65000) {
		t.Fatalf("bgp_asn = %#v, want 65000", got)
	}

	toGateway := relationshipByType(t, envelopes, awscloud.RelationshipDirectConnectVirtualInterfaceToGateway)
	if got, _ := toGateway.Payload["target_resource_id"].(string); got != "dxgw-1" {
		t.Fatalf("vif->gateway target = %q", got)
	}
	if got, _ := toGateway.Payload["target_type"].(string); got != awscloud.ResourceTypeDirectConnectGateway {
		t.Fatalf("vif->gateway target_type = %q", got)
	}

	toConnection := relationshipByType(t, envelopes, awscloud.RelationshipDirectConnectVirtualInterfaceToConnection)
	if got, _ := toConnection.Payload["target_resource_id"].(string); got != "dxcon-1" {
		t.Fatalf("vif->connection target = %q", got)
	}
	if got, _ := toConnection.Payload["target_type"].(string); got != awscloud.ResourceTypeDirectConnectConnection {
		t.Fatalf("vif->connection target_type = %q", got)
	}
}

// TestVirtualInterfaceWithoutGatewayEmitsNoGatewayEdge is the negative case: a
// public virtual interface has no Direct Connect gateway, so no gateway edge is
// fabricated.
func TestVirtualInterfaceWithoutGatewayEmitsNoGatewayEdge(t *testing.T) {
	envelopes := scanFixture(t, fakeClient{
		virtualInterfaces: []VirtualInterface{{
			ID:    "dxvif-public",
			Type:  "public",
			State: "available",
			VLAN:  200,
		}},
	})

	resourceByType(t, envelopes, awscloud.ResourceTypeDirectConnectVirtualInterface)
	if hasRelationship(envelopes, awscloud.RelationshipDirectConnectVirtualInterfaceToGateway) {
		t.Fatal("public virtual interface fabricated a gateway edge")
	}
	if hasRelationship(envelopes, awscloud.RelationshipDirectConnectVirtualInterfaceToConnection) {
		t.Fatal("virtual interface without a connection fabricated a connection edge")
	}
}

// TestScannerEmitsGatewayMatchingTransitGatewayEdgeTarget is the dangling-edge
// closer: the transitgateway scanner emits an edge whose target_type is
// aws_direct_connect_gateway and target_resource_id is the bare DX gateway ID.
// This pins that the Direct Connect gateway resource this scanner emits uses
// exactly that resource_type and the bare ID as resource_id, so the edge joins.
func TestScannerEmitsGatewayMatchingTransitGatewayEdgeTarget(t *testing.T) {
	envelopes := scanFixture(t, fakeClient{
		gateways: []Gateway{{
			ID:            "dxgw-1",
			Name:          "core-dxgw",
			State:         "available",
			OwnerAccount:  "123456789012",
			AmazonSideASN: 64512,
		}},
	})

	gw := resourceByType(t, envelopes, awscloud.ResourceTypeDirectConnectGateway)
	if got, _ := gw.Payload["resource_type"].(string); got != "aws_direct_connect_gateway" {
		t.Fatalf("gateway resource_type = %q, want aws_direct_connect_gateway (matches TGW edge target_type)", got)
	}
	if got, _ := gw.Payload["resource_id"].(string); got != "dxgw-1" {
		t.Fatalf("gateway resource_id = %q, want bare dxgw-1 (matches TGW edge target_resource_id)", got)
	}
}

// TestScannerEmitsGatewayToTransitGatewayAssociation is the positive
// hybrid-networking case: a Direct Connect gateway association to a transit
// gateway must target the transitgateway-scanner-owned aws_ec2_transit_gateway
// node by its bare tgw- ID.
func TestScannerEmitsGatewayToTransitGatewayAssociation(t *testing.T) {
	envelopes := scanFixture(t, fakeClient{
		gateways: []Gateway{{ID: "dxgw-1", State: "available"}},
		gatewayAssociations: []GatewayAssociation{{
			GatewayID:             "dxgw-1",
			AssociationID:         "dxgw-assoc-1",
			AssociationState:      "associated",
			AssociatedGatewayID:   "tgw-1",
			AssociatedGatewayType: "transitGateway",
		}},
	})

	edge := relationshipByType(t, envelopes, awscloud.RelationshipDirectConnectGatewayToTransitGateway)
	if got, _ := edge.Payload["source_resource_id"].(string); got != "dxgw-1" {
		t.Fatalf("dxgw->tgw source = %q", got)
	}
	if got, _ := edge.Payload["target_resource_id"].(string); got != "tgw-1" {
		t.Fatalf("dxgw->tgw target = %q", got)
	}
	if got, _ := edge.Payload["target_type"].(string); got != awscloud.ResourceTypeTransitGateway {
		t.Fatalf("dxgw->tgw target_type = %q, want %q", got, awscloud.ResourceTypeTransitGateway)
	}
}

// TestScannerEmitsGatewayToVPNGatewayAssociation pins the virtual-private-gateway
// association to the vpc-scanner-owned aws_vpc_vpn_gateway node by bare vgw- ID.
func TestScannerEmitsGatewayToVPNGatewayAssociation(t *testing.T) {
	envelopes := scanFixture(t, fakeClient{
		gateways: []Gateway{{ID: "dxgw-1", State: "available"}},
		gatewayAssociations: []GatewayAssociation{{
			GatewayID:             "dxgw-1",
			AssociationID:         "dxgw-assoc-2",
			AssociationState:      "associated",
			AssociatedGatewayID:   "vgw-1",
			AssociatedGatewayType: "virtualPrivateGateway",
		}},
	})

	edge := relationshipByType(t, envelopes, awscloud.RelationshipDirectConnectGatewayToVPNGateway)
	if got, _ := edge.Payload["target_resource_id"].(string); got != "vgw-1" {
		t.Fatalf("dxgw->vgw target = %q", got)
	}
	if got, _ := edge.Payload["target_type"].(string); got != awscloud.ResourceTypeVPCVPNGateway {
		t.Fatalf("dxgw->vgw target_type = %q, want %q", got, awscloud.ResourceTypeVPCVPNGateway)
	}
}

// TestGatewayAssociationUnknownTypeEmitsNoEdge is the negative case for an
// association whose associated gateway type is neither transit nor virtual
// private gateway: no edge is fabricated.
func TestGatewayAssociationUnknownTypeEmitsNoEdge(t *testing.T) {
	envelopes := scanFixture(t, fakeClient{
		gateways: []Gateway{{ID: "dxgw-1", State: "available"}},
		gatewayAssociations: []GatewayAssociation{{
			GatewayID:             "dxgw-1",
			AssociatedGatewayID:   "unknown-1",
			AssociatedGatewayType: "someFutureGateway",
		}},
	})

	for _, forbidden := range []string{
		awscloud.RelationshipDirectConnectGatewayToTransitGateway,
		awscloud.RelationshipDirectConnectGatewayToVPNGateway,
	} {
		if hasRelationship(envelopes, forbidden) {
			t.Fatalf("unknown associated gateway type fabricated edge %q", forbidden)
		}
	}
}

// TestClientInterfaceIsReadOnly pins the scanner-owned Client contract to
// read-only methods. The issue forbids any mutation of Direct Connect
// resources. Any method whose name begins with a mutation verb fails this test
// before the scanner can be compiled.
func TestClientInterfaceIsReadOnly(t *testing.T) {
	clientType := reflect.TypeOf((*Client)(nil)).Elem()
	forbiddenPrefixes := []string{
		"Create", "Delete", "Update", "Associate", "Disassociate",
		"Accept", "Reject", "Enable", "Disable", "Confirm", "Allocate",
		"Tag", "Untag", "Modify", "Start", "Stop",
	}
	for i := 0; i < clientType.NumMethod(); i++ {
		method := clientType.Method(i)
		for _, prefix := range forbiddenPrefixes {
			if strings.HasPrefix(method.Name, prefix) {
				t.Fatalf("Client method %q has forbidden prefix %q; Direct Connect Client must be read-only", method.Name, prefix)
			}
		}
		if !strings.HasPrefix(method.Name, "List") {
			t.Fatalf("Client method %q is not a List read", method.Name)
		}
	}
}

// TestVirtualInterfaceTypeNeverCarriesAuthKey is the security-review exclusion:
// the scanner-owned VirtualInterface type must have no field that could hold a
// BGP authentication key, so the authKey AWS reports can never be persisted.
func TestVirtualInterfaceTypeNeverCarriesAuthKey(t *testing.T) {
	vifType := reflect.TypeOf(VirtualInterface{})
	for i := 0; i < vifType.NumField(); i++ {
		name := strings.ToLower(vifType.Field(i).Name)
		if strings.Contains(name, "auth") || strings.Contains(name, "key") || strings.Contains(name, "secret") || strings.Contains(name, "password") {
			t.Fatalf("VirtualInterface exposes field %q; BGP auth key material must never be persisted", vifType.Field(i).Name)
		}
	}
}

// TestConnectionAndLAGNeverCarryMacSecKeyMaterial pins that Connection and LAG
// never carry MACsec connectivity association key names (CKN) or secret ARNs.
// Only the boolean MacSecCapable capability flag is allowed.
func TestConnectionAndLAGNeverCarryMacSecKeyMaterial(t *testing.T) {
	for _, value := range []any{Connection{}, LAG{}} {
		typ := reflect.TypeOf(value)
		for i := 0; i < typ.NumField(); i++ {
			name := strings.ToLower(typ.Field(i).Name)
			for _, forbidden := range []string{"ckn", "secret", "macseckey", "macseckeys"} {
				if strings.Contains(name, forbidden) {
					t.Fatalf("%s exposes field %q; MACsec key material must never be persisted", typ.Name(), typ.Field(i).Name)
				}
			}
		}
	}
}

// TestVirtualInterfaceAttributesNeverIncludeAuthKey pins that emitted virtual
// interface attribute payloads never carry an auth-key-shaped attribute key.
func TestVirtualInterfaceAttributesNeverIncludeAuthKey(t *testing.T) {
	envelopes := scanFixture(t, fakeClient{
		virtualInterfaces: []VirtualInterface{{
			ID:    "dxvif-1",
			Type:  "private",
			State: "available",
			VLAN:  101,
			ASN:   65000,
		}},
	})
	vif := resourceByType(t, envelopes, awscloud.ResourceTypeDirectConnectVirtualInterface)
	attributes := attributesOf(t, vif)
	for key := range attributes {
		lower := strings.ToLower(key)
		if strings.Contains(lower, "auth") || strings.Contains(lower, "secret") || strings.Contains(lower, "password") {
			t.Fatalf("virtual interface attribute %q is auth/secret-shaped; must never be emitted", key)
		}
	}
}

// TestResourceTypesDisjointFromOtherScanners pins the boundary: the Direct
// Connect scanner owns the aws_direct_connect_* types and must not claim the
// transit-gateway or VPN-gateway nodes it only references as relationship
// targets.
func TestResourceTypesDisjointFromOtherScanners(t *testing.T) {
	foreign := map[string]struct{}{
		awscloud.ResourceTypeTransitGateway: {},
		awscloud.ResourceTypeVPCVPNGateway:  {},
	}
	emitted := []string{
		awscloud.ResourceTypeDirectConnectConnection,
		awscloud.ResourceTypeDirectConnectVirtualInterface,
		awscloud.ResourceTypeDirectConnectGateway,
		awscloud.ResourceTypeDirectConnectLAG,
	}
	seen := map[string]struct{}{}
	for _, resourceType := range emitted {
		if _, owned := foreign[resourceType]; owned {
			t.Fatalf("Direct Connect scanner claims foreign resource_type %q", resourceType)
		}
		if _, dup := seen[resourceType]; dup {
			t.Fatalf("duplicate emitted resource_type %q", resourceType)
		}
		seen[resourceType] = struct{}{}
	}
}
