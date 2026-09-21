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
// apiClient interface against any AWS EC2 method that would let the Transit
// Gateway scanner mutate transit gateways, attachments, route tables, or
// multicast domains, or change route-table association and propagation. Issue
// #732 names these mutation APIs explicitly. Adding any of them to apiClient
// (directly or by embedding a new AWS SDK paginator interface that pulls them
// in) fails this test before the change can be compiled into production.
func TestAPIClientNeverIncludesForbiddenMethods(t *testing.T) {
	apiClientType := reflect.TypeOf((*apiClient)(nil)).Elem()
	forbidden := []string{
		// Issue #732 explicit blocklist.
		"CreateTransitGateway",
		"DeleteTransitGateway",
		"ModifyTransitGateway",
		"CreateTransitGatewayVpcAttachment",
		"DeleteTransitGatewayVpcAttachment",
		"ModifyTransitGatewayVpcAttachment",
		"CreateTransitGatewayPeeringAttachment",
		"DeleteTransitGatewayPeeringAttachment",
		"AcceptTransitGatewayPeeringAttachment",
		"RejectTransitGatewayPeeringAttachment",
		"CreateTransitGatewayConnect",
		"DeleteTransitGatewayConnect",
		"CreateTransitGatewayConnectPeer",
		"DeleteTransitGatewayConnectPeer",
		"AcceptTransitGatewayVpcAttachment",
		"RejectTransitGatewayVpcAttachment",
		"CreateTransitGatewayRouteTable",
		"DeleteTransitGatewayRouteTable",
		"CreateTransitGatewayRoute",
		"DeleteTransitGatewayRoute",
		"ReplaceTransitGatewayRoute",
		"CreateTransitGatewayMulticastDomain",
		"DeleteTransitGatewayMulticastDomain",
		"AcceptTransitGatewayMulticastDomainAssociations",
		"RejectTransitGatewayMulticastDomainAssociations",
		"CreateTransitGatewayPolicyTable",
		"DeleteTransitGatewayPolicyTable",
		"AssociateTransitGatewayRouteTable",
		"DisassociateTransitGatewayRouteTable",
		"AssociateTransitGatewayPolicyTable",
		"DisassociateTransitGatewayPolicyTable",
		"AssociateTransitGatewayMulticastDomain",
		"DisassociateTransitGatewayMulticastDomain",
		"EnableTransitGatewayRouteTablePropagation",
		"DisableTransitGatewayRouteTablePropagation",
		"RegisterTransitGatewayMulticastGroupMembers",
		"DeregisterTransitGatewayMulticastGroupMembers",
		"RegisterTransitGatewayMulticastGroupSources",
		"DeregisterTransitGatewayMulticastGroupSources",
		"CreateTags",
		"DeleteTags",
		"TagResource",
		"UntagResource",
		// Reading routes or policy entries is out of scope; these would expose
		// network policy detail beyond inventory metadata.
		"SearchTransitGatewayRoutes",
		"GetTransitGatewayRouteTableAssociations",
		"GetTransitGatewayRouteTablePropagations",
		"GetTransitGatewayPolicyTableEntries",
		"GetTransitGatewayMulticastDomainAssociations",
	}
	for _, method := range forbidden {
		if _, ok := apiClientType.MethodByName(method); ok {
			t.Fatalf("apiClient declares forbidden EC2 transit gateway API %q; the adapter must not gain access to that method", method)
		}
	}
}

// TestAPIClientOnlyReadsDescribes locks the adapter's method set so a future
// change that adds a mutation by embedding a new SDK interface fails even if it
// is not in the explicit forbidden list above. The Transit Gateway adapter only
// uses Describe* paginators.
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

func TestMapTransitGatewayMapsIdentityAndOptions(t *testing.T) {
	gateway := mapTransitGateway(awsec2types.TransitGateway{
		TransitGatewayId:  aws.String("tgw-1"),
		TransitGatewayArn: aws.String("arn:aws:ec2:us-east-1:123456789012:transit-gateway/tgw-1"),
		OwnerId:           aws.String("123456789012"),
		State:             awsec2types.TransitGatewayStateAvailable,
		Description:       aws.String("core hub"),
		CreationTime:      aws.Time(time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)),
		Options: &awsec2types.TransitGatewayOptions{
			AmazonSideAsn:                  aws.Int64(64512),
			AssociationDefaultRouteTableId: aws.String("tgw-rtb-assoc"),
			DefaultRouteTableAssociation:   awsec2types.DefaultRouteTableAssociationValueEnable,
			DnsSupport:                     awsec2types.DnsSupportValueEnable,
		},
		Tags: []awsec2types.Tag{{Key: aws.String("Name"), Value: aws.String("core-tgw")}},
	})

	if gateway.ID != "tgw-1" {
		t.Fatalf("ID = %q", gateway.ID)
	}
	if gateway.State != "available" {
		t.Fatalf("State = %q", gateway.State)
	}
	if gateway.Options.AmazonSideASN != 64512 {
		t.Fatalf("AmazonSideASN = %d", gateway.Options.AmazonSideASN)
	}
	if gateway.Options.DefaultRouteTableAssociation != "enable" {
		t.Fatalf("DefaultRouteTableAssociation = %q", gateway.Options.DefaultRouteTableAssociation)
	}
	if gateway.Tags["Name"] != "core-tgw" {
		t.Fatalf("tag Name = %q", gateway.Tags["Name"])
	}
}

func TestMapAttachmentMapsAssociationAndResource(t *testing.T) {
	attachment := mapAttachment(awsec2types.TransitGatewayAttachment{
		TransitGatewayAttachmentId: aws.String("tgw-attach-1"),
		TransitGatewayId:           aws.String("tgw-1"),
		TransitGatewayOwnerId:      aws.String("123456789012"),
		ResourceType:               awsec2types.TransitGatewayAttachmentResourceTypeVpc,
		ResourceId:                 aws.String("vpc-1"),
		ResourceOwnerId:            aws.String("123456789012"),
		State:                      awsec2types.TransitGatewayAttachmentStateAvailable,
		Association: &awsec2types.TransitGatewayAttachmentAssociation{
			TransitGatewayRouteTableId: aws.String("tgw-rtb-1"),
			State:                      awsec2types.TransitGatewayAssociationStateAssociated,
		},
	})

	if attachment.ResourceType != "vpc" {
		t.Fatalf("ResourceType = %q", attachment.ResourceType)
	}
	if attachment.ResourceID != "vpc-1" {
		t.Fatalf("ResourceID = %q", attachment.ResourceID)
	}
	if attachment.AssociationRouteTableID != "tgw-rtb-1" {
		t.Fatalf("AssociationRouteTableID = %q", attachment.AssociationRouteTableID)
	}
	if attachment.AssociationState != "associated" {
		t.Fatalf("AssociationState = %q", attachment.AssociationState)
	}
}

func TestMapPeeringAttachmentMapsBothSides(t *testing.T) {
	peering := mapPeeringAttachment(awsec2types.TransitGatewayPeeringAttachment{
		TransitGatewayAttachmentId: aws.String("tgw-attach-peer"),
		State:                      awsec2types.TransitGatewayAttachmentStateAvailable,
		Status: &awsec2types.PeeringAttachmentStatus{
			Code:    aws.String("available"),
			Message: aws.String("ok"),
		},
		RequesterTgwInfo: &awsec2types.PeeringTgwInfo{
			TransitGatewayId: aws.String("tgw-1"),
			OwnerId:          aws.String("123456789012"),
			Region:           aws.String("us-east-1"),
		},
		AccepterTgwInfo: &awsec2types.PeeringTgwInfo{
			TransitGatewayId: aws.String("tgw-remote"),
			OwnerId:          aws.String("999988887777"),
			Region:           aws.String("eu-west-1"),
		},
	})

	if peering.Requester.TransitGatewayID != "tgw-1" {
		t.Fatalf("requester tgw = %q", peering.Requester.TransitGatewayID)
	}
	if peering.Accepter.TransitGatewayID != "tgw-remote" {
		t.Fatalf("accepter tgw = %q", peering.Accepter.TransitGatewayID)
	}
	if peering.Accepter.OwnerID != "999988887777" {
		t.Fatalf("accepter owner = %q; cross-account peer must be surfaced as reported", peering.Accepter.OwnerID)
	}
	if peering.StatusCode != "available" {
		t.Fatalf("status code = %q", peering.StatusCode)
	}
}

func TestMapPolicyTableMapsIdentityOnly(t *testing.T) {
	// The scanner-owned PolicyTable type must not carry policy rule entries;
	// the SDK adapter only reads identity and state. This test pins that the
	// mapped type has no Entries/Rules field.
	policyTable := mapPolicyTable(awsec2types.TransitGatewayPolicyTable{
		TransitGatewayPolicyTableId: aws.String("tgw-pt-1"),
		TransitGatewayId:            aws.String("tgw-1"),
		State:                       awsec2types.TransitGatewayPolicyTableStateAvailable,
	})
	mappedType := reflect.TypeOf(policyTable)
	for _, forbidden := range []string{"Entries", "Rules", "Policy"} {
		if _, ok := mappedType.FieldByName(forbidden); ok {
			t.Fatalf("PolicyTable mapped type exposes %q; policy rules are out of the inventory contract", forbidden)
		}
	}
	if policyTable.ID != "tgw-pt-1" {
		t.Fatalf("ID = %q", policyTable.ID)
	}
}
