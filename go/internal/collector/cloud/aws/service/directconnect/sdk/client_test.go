// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"reflect"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	dxtypes "github.com/aws/aws-sdk-go-v2/service/directconnect/types"
)

// TestAPIClientNeverIncludesForbiddenMethods locks the SDK adapter's narrow
// apiClient interface against any AWS Direct Connect method that would let the
// scanner mutate connections, virtual interfaces, gateways, LAGs, or
// gateway associations, or confirm/allocate hosted resources. Adding any of
// them to apiClient fails this test before the change can be compiled into
// production.
func TestAPIClientNeverIncludesForbiddenMethods(t *testing.T) {
	apiClientType := reflect.TypeOf((*apiClient)(nil)).Elem()
	forbidden := []string{
		"CreateConnection",
		"DeleteConnection",
		"UpdateConnection",
		"CreatePrivateVirtualInterface",
		"CreatePublicVirtualInterface",
		"CreateTransitVirtualInterface",
		"DeleteVirtualInterface",
		"UpdateVirtualInterfaceAttributes",
		"AssociateVirtualInterface",
		"ConfirmPrivateVirtualInterface",
		"ConfirmPublicVirtualInterface",
		"ConfirmTransitVirtualInterface",
		"AllocatePrivateVirtualInterface",
		"AllocatePublicVirtualInterface",
		"AllocateTransitVirtualInterface",
		"AllocateConnectionOnInterconnect",
		"AllocateHostedConnection",
		"CreateDirectConnectGateway",
		"DeleteDirectConnectGateway",
		"UpdateDirectConnectGateway",
		"CreateDirectConnectGatewayAssociation",
		"DeleteDirectConnectGatewayAssociation",
		"UpdateDirectConnectGatewayAssociation",
		"AcceptDirectConnectGatewayAssociationProposal",
		"CreateDirectConnectGatewayAssociationProposal",
		"DeleteDirectConnectGatewayAssociationProposal",
		"CreateLag",
		"DeleteLag",
		"UpdateLag",
		"AssociateConnectionWithLag",
		"DisassociateConnectionFromLag",
		"AssociateHostedConnection",
		"AssociateMacSecKey",
		"DisassociateMacSecKey",
		"UpdateConnectionMacSecKey",
		"ConfirmConnection",
		"TagResource",
		"UntagResource",
		"StartBgpFailoverTest",
		"StopBgpFailoverTest",
		// Reading a virtual interface router configuration would surface the
		// BGP auth key in the rendered config; never reachable.
		"DescribeRouterConfiguration",
	}
	for _, method := range forbidden {
		if _, ok := apiClientType.MethodByName(method); ok {
			t.Fatalf("apiClient declares forbidden Direct Connect API %q; the adapter must not gain access to that method", method)
		}
	}
}

// TestAPIClientOnlyReadsDescribes locks the adapter's method set so a future
// change that adds a mutation by embedding a new method fails even if it is not
// in the explicit forbidden list above. The Direct Connect adapter only uses
// the Describe read surface, and not every Describe is allowed
// (DescribeRouterConfiguration is excluded above because it renders the auth
// key).
func TestAPIClientOnlyReadsDescribes(t *testing.T) {
	apiClientType := reflect.TypeOf((*apiClient)(nil)).Elem()
	if apiClientType.NumMethod() == 0 {
		t.Fatal("apiClient has no methods; expected the Describe read surface")
	}
	for i := 0; i < apiClientType.NumMethod(); i++ {
		method := apiClientType.Method(i)
		if !strings.HasPrefix(method.Name, "Describe") {
			t.Fatalf("apiClient method %q is not a Describe read", method.Name)
		}
	}
}

func TestMapVirtualInterfaceDropsAuthKey(t *testing.T) {
	vif := mapVirtualInterface(dxtypes.VirtualInterface{
		VirtualInterfaceId:     aws.String("dxvif-1"),
		VirtualInterfaceName:   aws.String("prod-vif"),
		VirtualInterfaceType:   aws.String("transit"),
		VirtualInterfaceState:  dxtypes.VirtualInterfaceStateAvailable,
		ConnectionId:           aws.String("dxcon-1"),
		DirectConnectGatewayId: aws.String("dxgw-1"),
		VirtualGatewayId:       aws.String("vgw-1"),
		Vlan:                   101,
		Asn:                    65000,
		AmazonSideAsn:          aws.Int64(64512),
		AddressFamily:          dxtypes.AddressFamilyIPv4,
		AuthKey:                aws.String("super-secret-bgp-md5-key"),
		OwnerAccount:           aws.String("123456789012"),
		Location:               aws.String("EqDC2"),
	})

	if vif.ID != "dxvif-1" {
		t.Fatalf("ID = %q", vif.ID)
	}
	if vif.Type != "transit" {
		t.Fatalf("Type = %q", vif.Type)
	}
	if vif.State != "available" {
		t.Fatalf("State = %q", vif.State)
	}
	if vif.GatewayID != "dxgw-1" {
		t.Fatalf("GatewayID = %q", vif.GatewayID)
	}
	if vif.ASN != 65000 {
		t.Fatalf("ASN = %d", vif.ASN)
	}
	// The mapped type cannot hold an auth key by construction; this is the
	// belt-and-suspenders proof that even when AWS returns one, nothing in the
	// scanner-owned struct can carry it.
	mappedType := reflect.TypeOf(vif)
	for i := 0; i < mappedType.NumField(); i++ {
		name := strings.ToLower(mappedType.Field(i).Name)
		if strings.Contains(name, "auth") || strings.Contains(name, "key") || strings.Contains(name, "secret") {
			t.Fatalf("mapped VirtualInterface exposes field %q; auth key material must never be mapped", mappedType.Field(i).Name)
		}
	}
}

func TestMapConnectionDropsMacSecKeys(t *testing.T) {
	connection := mapConnection(dxtypes.Connection{
		ConnectionId:    aws.String("dxcon-1"),
		ConnectionName:  aws.String("primary-port"),
		ConnectionState: dxtypes.ConnectionStateAvailable,
		Location:        aws.String("EqDC2"),
		Bandwidth:       aws.String("10Gbps"),
		Region:          aws.String("us-east-1"),
		PartnerName:     aws.String("Example Partner"),
		ProviderName:    aws.String("Example Provider"),
		LagId:           aws.String("dxlag-1"),
		Vlan:            0,
		MacSecCapable:   aws.Bool(true),
		MacSecKeys: []dxtypes.MacSecKey{{
			Ckn:       aws.String("0011223344"),
			SecretARN: aws.String("arn:aws:secretsmanager:us-east-1:123456789012:secret:dx-macsec"),
		}},
	})

	if connection.ID != "dxcon-1" {
		t.Fatalf("ID = %q", connection.ID)
	}
	if !connection.MacSecCapable {
		t.Fatalf("MacSecCapable = false, want true")
	}
	mappedType := reflect.TypeOf(connection)
	for i := 0; i < mappedType.NumField(); i++ {
		name := strings.ToLower(mappedType.Field(i).Name)
		for _, forbidden := range []string{"ckn", "secret", "macseckey"} {
			if strings.Contains(name, forbidden) {
				t.Fatalf("mapped Connection exposes field %q; MACsec key material must never be mapped", mappedType.Field(i).Name)
			}
		}
	}
}

func TestMapGatewayAssociationMapsAssociatedGateway(t *testing.T) {
	association := mapGatewayAssociation(dxtypes.DirectConnectGatewayAssociation{
		DirectConnectGatewayId: aws.String("dxgw-1"),
		AssociationId:          aws.String("dxgw-assoc-1"),
		AssociationState:       dxtypes.DirectConnectGatewayAssociationStateAssociated,
		AssociatedGateway: &dxtypes.AssociatedGateway{
			Id:   aws.String("tgw-1"),
			Type: dxtypes.GatewayTypeTransitGateway,
		},
	})

	if association.GatewayID != "dxgw-1" {
		t.Fatalf("GatewayID = %q", association.GatewayID)
	}
	if association.AssociatedGatewayID != "tgw-1" {
		t.Fatalf("AssociatedGatewayID = %q", association.AssociatedGatewayID)
	}
	if association.AssociatedGatewayType != "transitGateway" {
		t.Fatalf("AssociatedGatewayType = %q", association.AssociatedGatewayType)
	}
}
