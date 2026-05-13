package awssdk

import (
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"

	ec2service "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/ec2"
)

const ec2InstanceTargetType = "aws_ec2_instance"

func mapVPC(vpc awsec2types.Vpc) ec2service.VPC {
	return ec2service.VPC{
		ID:              aws.ToString(vpc.VpcId),
		OwnerID:         aws.ToString(vpc.OwnerId),
		State:           string(vpc.State),
		CIDRBlock:       aws.ToString(vpc.CidrBlock),
		DHCPOptionsID:   aws.ToString(vpc.DhcpOptionsId),
		InstanceTenancy: string(vpc.InstanceTenancy),
		IsDefault:       aws.ToBool(vpc.IsDefault),
		IPv4CIDRBlocks:  mapVPCCIDRBlocks(vpc.CidrBlockAssociationSet),
		IPv6CIDRBlocks:  mapVPCIPv6CIDRBlocks(vpc.Ipv6CidrBlockAssociationSet),
		Tags:            mapTags(vpc.Tags),
	}
}

func mapSubnet(subnet awsec2types.Subnet) ec2service.Subnet {
	return ec2service.Subnet{
		ARN:                       aws.ToString(subnet.SubnetArn),
		ID:                        aws.ToString(subnet.SubnetId),
		VPCID:                     aws.ToString(subnet.VpcId),
		OwnerID:                   aws.ToString(subnet.OwnerId),
		State:                     string(subnet.State),
		CIDRBlock:                 aws.ToString(subnet.CidrBlock),
		AvailabilityZone:          aws.ToString(subnet.AvailabilityZone),
		AvailabilityZoneID:        aws.ToString(subnet.AvailabilityZoneId),
		AvailableIPAddressCount:   aws.ToInt32(subnet.AvailableIpAddressCount),
		DefaultForAZ:              aws.ToBool(subnet.DefaultForAz),
		MapPublicIPOnLaunch:       aws.ToBool(subnet.MapPublicIpOnLaunch),
		AssignIPv6AddressOnCreate: aws.ToBool(subnet.AssignIpv6AddressOnCreation),
		IPv6Native:                aws.ToBool(subnet.Ipv6Native),
		OutpostARN:                aws.ToString(subnet.OutpostArn),
		IPv6CIDRBlocks:            mapSubnetIPv6CIDRBlocks(subnet.Ipv6CidrBlockAssociationSet),
		Tags:                      mapTags(subnet.Tags),
	}
}

func mapSecurityGroup(group awsec2types.SecurityGroup) ec2service.SecurityGroup {
	return ec2service.SecurityGroup{
		ID:          aws.ToString(group.GroupId),
		Name:        aws.ToString(group.GroupName),
		Description: aws.ToString(group.Description),
		VPCID:       aws.ToString(group.VpcId),
		OwnerID:     aws.ToString(group.OwnerId),
		Tags:        mapTags(group.Tags),
	}
}

func mapSecurityGroupRule(rule awsec2types.SecurityGroupRule) ec2service.SecurityGroupRule {
	return ec2service.SecurityGroupRule{
		ID:              aws.ToString(rule.SecurityGroupRuleId),
		GroupID:         aws.ToString(rule.GroupId),
		GroupOwnerID:    aws.ToString(rule.GroupOwnerId),
		IsEgress:        aws.ToBool(rule.IsEgress),
		Protocol:        aws.ToString(rule.IpProtocol),
		FromPort:        rule.FromPort,
		ToPort:          rule.ToPort,
		CIDRIPv4:        aws.ToString(rule.CidrIpv4),
		CIDRIPv6:        aws.ToString(rule.CidrIpv6),
		PrefixListID:    aws.ToString(rule.PrefixListId),
		ReferencedGroup: mapReferencedSecurityGroup(rule.ReferencedGroupInfo),
		Description:     aws.ToString(rule.Description),
		Tags:            mapTags(rule.Tags),
	}
}

func mapNetworkInterface(
	region string,
	accountID string,
	networkInterface awsec2types.NetworkInterface,
) ec2service.NetworkInterface {
	return ec2service.NetworkInterface{
		ID:                 aws.ToString(networkInterface.NetworkInterfaceId),
		VPCID:              aws.ToString(networkInterface.VpcId),
		SubnetID:           aws.ToString(networkInterface.SubnetId),
		OwnerID:            aws.ToString(networkInterface.OwnerId),
		Status:             string(networkInterface.Status),
		InterfaceType:      string(networkInterface.InterfaceType),
		Description:        aws.ToString(networkInterface.Description),
		AvailabilityZone:   aws.ToString(networkInterface.AvailabilityZone),
		MacAddress:         aws.ToString(networkInterface.MacAddress),
		PrivateDNSName:     aws.ToString(networkInterface.PrivateDnsName),
		PrivateIPAddress:   aws.ToString(networkInterface.PrivateIpAddress),
		RequesterID:        aws.ToString(networkInterface.RequesterId),
		RequesterManaged:   aws.ToBool(networkInterface.RequesterManaged),
		SourceDestCheck:    aws.ToBool(networkInterface.SourceDestCheck),
		SecurityGroups:     mapSecurityGroupRefs(networkInterface.Groups),
		PrivateIPAddresses: mapPrivateIPAddresses(networkInterface.PrivateIpAddresses),
		IPv6Addresses:      mapIPv6Addresses(networkInterface.Ipv6Addresses),
		Attachment:         mapAttachment(region, accountID, networkInterface.Attachment),
		Tags:               mapTags(networkInterface.TagSet),
	}
}

func mapVPCCIDRBlocks(input []awsec2types.VpcCidrBlockAssociation) []ec2service.CIDRBlockAssociation {
	if len(input) == 0 {
		return nil
	}
	output := make([]ec2service.CIDRBlockAssociation, 0, len(input))
	for _, association := range input {
		state := ""
		if association.CidrBlockState != nil {
			state = string(association.CidrBlockState.State)
		}
		output = append(output, ec2service.CIDRBlockAssociation{
			AssociationID: aws.ToString(association.AssociationId),
			CIDRBlock:     aws.ToString(association.CidrBlock),
			State:         state,
		})
	}
	return output
}

func mapVPCIPv6CIDRBlocks(input []awsec2types.VpcIpv6CidrBlockAssociation) []ec2service.IPv6CIDRBlockAssociation {
	if len(input) == 0 {
		return nil
	}
	output := make([]ec2service.IPv6CIDRBlockAssociation, 0, len(input))
	for _, association := range input {
		state := ""
		if association.Ipv6CidrBlockState != nil {
			state = string(association.Ipv6CidrBlockState.State)
		}
		output = append(output, ec2service.IPv6CIDRBlockAssociation{
			AssociationID:      aws.ToString(association.AssociationId),
			CIDRBlock:          aws.ToString(association.Ipv6CidrBlock),
			State:              state,
			IPv6Pool:           aws.ToString(association.Ipv6Pool),
			NetworkBorderGroup: aws.ToString(association.NetworkBorderGroup),
		})
	}
	return output
}

func mapSubnetIPv6CIDRBlocks(input []awsec2types.SubnetIpv6CidrBlockAssociation) []ec2service.CIDRBlockAssociation {
	if len(input) == 0 {
		return nil
	}
	output := make([]ec2service.CIDRBlockAssociation, 0, len(input))
	for _, association := range input {
		state := ""
		if association.Ipv6CidrBlockState != nil {
			state = string(association.Ipv6CidrBlockState.State)
		}
		output = append(output, ec2service.CIDRBlockAssociation{
			AssociationID: aws.ToString(association.AssociationId),
			CIDRBlock:     aws.ToString(association.Ipv6CidrBlock),
			State:         state,
		})
	}
	return output
}

func mapReferencedSecurityGroup(input *awsec2types.ReferencedSecurityGroup) *ec2service.ReferencedSecurityGroup {
	if input == nil {
		return nil
	}
	return &ec2service.ReferencedSecurityGroup{
		GroupID:                aws.ToString(input.GroupId),
		UserID:                 aws.ToString(input.UserId),
		VPCID:                  aws.ToString(input.VpcId),
		PeeringStatus:          aws.ToString(input.PeeringStatus),
		VPCPeeringConnectionID: aws.ToString(input.VpcPeeringConnectionId),
	}
}

func mapAttachment(
	region string,
	accountID string,
	attachment *awsec2types.NetworkInterfaceAttachment,
) *ec2service.NetworkInterfaceAttachment {
	if attachment == nil {
		return nil
	}
	instanceID := aws.ToString(attachment.InstanceId)
	instanceOwnerID := firstNonEmpty(aws.ToString(attachment.InstanceOwnerId), accountID)
	return &ec2service.NetworkInterfaceAttachment{
		ID:                   aws.ToString(attachment.AttachmentId),
		InstanceID:           instanceID,
		InstanceOwnerID:      instanceOwnerID,
		AttachedResourceARN:  ec2InstanceARN(region, instanceOwnerID, instanceID),
		AttachedResourceType: attachedResourceType(instanceID),
		Status:               string(attachment.Status),
		AttachTime:           aws.ToTime(attachment.AttachTime),
		DeleteOnTermination:  aws.ToBool(attachment.DeleteOnTermination),
		DeviceIndex:          aws.ToInt32(attachment.DeviceIndex),
		NetworkCardIndex:     aws.ToInt32(attachment.NetworkCardIndex),
	}
}

func mapSecurityGroupRefs(input []awsec2types.GroupIdentifier) []ec2service.SecurityGroupRef {
	if len(input) == 0 {
		return nil
	}
	output := make([]ec2service.SecurityGroupRef, 0, len(input))
	for _, group := range input {
		output = append(output, ec2service.SecurityGroupRef{
			ID:   aws.ToString(group.GroupId),
			Name: aws.ToString(group.GroupName),
		})
	}
	return output
}

func mapPrivateIPAddresses(input []awsec2types.NetworkInterfacePrivateIpAddress) []ec2service.PrivateIPAddress {
	if len(input) == 0 {
		return nil
	}
	output := make([]ec2service.PrivateIPAddress, 0, len(input))
	for _, address := range input {
		output = append(output, ec2service.PrivateIPAddress{
			Address:        aws.ToString(address.PrivateIpAddress),
			PrivateDNSName: aws.ToString(address.PrivateDnsName),
			Primary:        aws.ToBool(address.Primary),
		})
	}
	return output
}

func mapIPv6Addresses(input []awsec2types.NetworkInterfaceIpv6Address) []string {
	if len(input) == 0 {
		return nil
	}
	output := make([]string, 0, len(input))
	for _, address := range input {
		if value := strings.TrimSpace(aws.ToString(address.Ipv6Address)); value != "" {
			output = append(output, value)
		}
	}
	return output
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
	return output
}

func ec2InstanceARN(region string, accountID string, instanceID string) string {
	region = strings.TrimSpace(region)
	accountID = strings.TrimSpace(accountID)
	instanceID = strings.TrimSpace(instanceID)
	if region == "" || accountID == "" || instanceID == "" {
		return ""
	}
	return "arn:aws:ec2:" + region + ":" + accountID + ":instance/" + instanceID
}

func attachedResourceType(instanceID string) string {
	if strings.TrimSpace(instanceID) == "" {
		return ""
	}
	return ec2InstanceTargetType
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
