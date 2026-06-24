// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"

	tgwservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/transitgateway"
)

func mapTransitGateway(gateway awsec2types.TransitGateway) tgwservice.TransitGateway {
	return tgwservice.TransitGateway{
		ID:          aws.ToString(gateway.TransitGatewayId),
		ARN:         aws.ToString(gateway.TransitGatewayArn),
		OwnerID:     aws.ToString(gateway.OwnerId),
		State:       string(gateway.State),
		Description: aws.ToString(gateway.Description),
		CreatedAt:   aws.ToTime(gateway.CreationTime),
		Options:     mapTransitGatewayOptions(gateway.Options),
		Tags:        mapTags(gateway.Tags),
	}
}

func mapTransitGatewayOptions(options *awsec2types.TransitGatewayOptions) tgwservice.TransitGatewayOptions {
	if options == nil {
		return tgwservice.TransitGatewayOptions{}
	}
	return tgwservice.TransitGatewayOptions{
		AmazonSideASN:                  aws.ToInt64(options.AmazonSideAsn),
		AssociationDefaultRouteTableID: aws.ToString(options.AssociationDefaultRouteTableId),
		PropagationDefaultRouteTableID: aws.ToString(options.PropagationDefaultRouteTableId),
		AutoAcceptSharedAttachments:    string(options.AutoAcceptSharedAttachments),
		DefaultRouteTableAssociation:   string(options.DefaultRouteTableAssociation),
		DefaultRouteTablePropagation:   string(options.DefaultRouteTablePropagation),
		DNSSupport:                     string(options.DnsSupport),
		MulticastSupport:               string(options.MulticastSupport),
		VPNECMPSupport:                 string(options.VpnEcmpSupport),
	}
}

func mapRouteTable(rt awsec2types.TransitGatewayRouteTable) tgwservice.RouteTable {
	return tgwservice.RouteTable{
		ID:                           aws.ToString(rt.TransitGatewayRouteTableId),
		TransitGatewayID:             aws.ToString(rt.TransitGatewayId),
		State:                        string(rt.State),
		DefaultAssociationRouteTable: aws.ToBool(rt.DefaultAssociationRouteTable),
		DefaultPropagationRouteTable: aws.ToBool(rt.DefaultPropagationRouteTable),
		CreatedAt:                    aws.ToTime(rt.CreationTime),
		Tags:                         mapTags(rt.Tags),
	}
}

func mapAttachment(attachment awsec2types.TransitGatewayAttachment) tgwservice.Attachment {
	associationRouteTableID := ""
	associationState := ""
	if attachment.Association != nil {
		associationRouteTableID = aws.ToString(attachment.Association.TransitGatewayRouteTableId)
		associationState = string(attachment.Association.State)
	}
	return tgwservice.Attachment{
		ID:                      aws.ToString(attachment.TransitGatewayAttachmentId),
		TransitGatewayID:        aws.ToString(attachment.TransitGatewayId),
		TransitGatewayOwnerID:   aws.ToString(attachment.TransitGatewayOwnerId),
		ResourceType:            string(attachment.ResourceType),
		ResourceID:              aws.ToString(attachment.ResourceId),
		ResourceOwnerID:         aws.ToString(attachment.ResourceOwnerId),
		State:                   string(attachment.State),
		AssociationRouteTableID: associationRouteTableID,
		AssociationState:        associationState,
		CreatedAt:               aws.ToTime(attachment.CreationTime),
		Tags:                    mapTags(attachment.Tags),
	}
}

func mapPeeringAttachment(peering awsec2types.TransitGatewayPeeringAttachment) tgwservice.PeeringAttachment {
	statusCode := ""
	statusMessage := ""
	if peering.Status != nil {
		statusCode = aws.ToString(peering.Status.Code)
		statusMessage = aws.ToString(peering.Status.Message)
	}
	return tgwservice.PeeringAttachment{
		ID:            aws.ToString(peering.TransitGatewayAttachmentId),
		State:         string(peering.State),
		StatusCode:    statusCode,
		StatusMessage: statusMessage,
		Requester:     mapPeeringInfo(peering.RequesterTgwInfo),
		Accepter:      mapPeeringInfo(peering.AccepterTgwInfo),
		CreatedAt:     aws.ToTime(peering.CreationTime),
		Tags:          mapTags(peering.Tags),
	}
}

func mapPeeringInfo(info *awsec2types.PeeringTgwInfo) tgwservice.PeeringTransitGatewayInfo {
	if info == nil {
		return tgwservice.PeeringTransitGatewayInfo{}
	}
	return tgwservice.PeeringTransitGatewayInfo{
		TransitGatewayID: aws.ToString(info.TransitGatewayId),
		OwnerID:          aws.ToString(info.OwnerId),
		Region:           aws.ToString(info.Region),
		CoreNetworkID:    aws.ToString(info.CoreNetworkId),
	}
}

func mapMulticastDomain(domain awsec2types.TransitGatewayMulticastDomain) tgwservice.MulticastDomain {
	return tgwservice.MulticastDomain{
		ID:               aws.ToString(domain.TransitGatewayMulticastDomainId),
		ARN:              aws.ToString(domain.TransitGatewayMulticastDomainArn),
		TransitGatewayID: aws.ToString(domain.TransitGatewayId),
		OwnerID:          aws.ToString(domain.OwnerId),
		State:            string(domain.State),
		CreatedAt:        aws.ToTime(domain.CreationTime),
		Options:          mapMulticastDomainOptions(domain.Options),
		Tags:             mapTags(domain.Tags),
	}
}

func mapMulticastDomainOptions(options *awsec2types.TransitGatewayMulticastDomainOptions) tgwservice.MulticastDomainOptions {
	if options == nil {
		return tgwservice.MulticastDomainOptions{}
	}
	return tgwservice.MulticastDomainOptions{
		AutoAcceptSharedAssociations: string(options.AutoAcceptSharedAssociations),
		IGMPv2Support:                string(options.Igmpv2Support),
		StaticSourcesSupport:         string(options.StaticSourcesSupport),
	}
}

func mapPolicyTable(policyTable awsec2types.TransitGatewayPolicyTable) tgwservice.PolicyTable {
	return tgwservice.PolicyTable{
		ID:               aws.ToString(policyTable.TransitGatewayPolicyTableId),
		TransitGatewayID: aws.ToString(policyTable.TransitGatewayId),
		State:            string(policyTable.State),
		CreatedAt:        aws.ToTime(policyTable.CreationTime),
		Tags:             mapTags(policyTable.Tags),
	}
}

func mapTags(tags []awsec2types.Tag) map[string]string {
	if len(tags) == 0 {
		return nil
	}
	output := make(map[string]string, len(tags))
	for _, tag := range tags {
		key := strings.TrimSpace(aws.ToString(tag.Key))
		if key == "" {
			continue
		}
		output[key] = aws.ToString(tag.Value)
	}
	if len(output) == 0 {
		return nil
	}
	return output
}
