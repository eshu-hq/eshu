package awssdk

import (
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

func TestMapVPCPreservesCIDRsAndTags(t *testing.T) {
	vpc := mapVPC(awsec2types.Vpc{
		CidrBlock:     aws.String("10.0.0.0/16"),
		DhcpOptionsId: aws.String("dopt-123"),
		IsDefault:     aws.Bool(true),
		OwnerId:       aws.String("123456789012"),
		State:         awsec2types.VpcStateAvailable,
		VpcId:         aws.String("vpc-123"),
		CidrBlockAssociationSet: []awsec2types.VpcCidrBlockAssociation{{
			AssociationId: aws.String("vpc-cidr-assoc-1"),
			CidrBlock:     aws.String("10.0.0.0/16"),
		}},
		Tags: []awsec2types.Tag{{Key: aws.String("env"), Value: aws.String("prod")}},
	})

	if vpc.ID != "vpc-123" {
		t.Fatalf("ID = %q", vpc.ID)
	}
	if vpc.Tags["env"] != "prod" {
		t.Fatalf("tag env = %q, want prod", vpc.Tags["env"])
	}
	if got := vpc.IPv4CIDRBlocks[0].CIDRBlock; got != "10.0.0.0/16" {
		t.Fatalf("CIDR block = %q", got)
	}
}

func TestMapSecurityGroupRulePreservesRuleTarget(t *testing.T) {
	fromPort := int32(443)
	toPort := int32(443)
	rule := mapSecurityGroupRule(awsec2types.SecurityGroupRule{
		CidrIpv4:            aws.String("0.0.0.0/0"),
		Description:         aws.String("https"),
		FromPort:            aws.Int32(fromPort),
		GroupId:             aws.String("sg-123"),
		GroupOwnerId:        aws.String("123456789012"),
		IpProtocol:          aws.String("tcp"),
		IsEgress:            aws.Bool(false),
		SecurityGroupRuleId: aws.String("sgr-123"),
		ToPort:              aws.Int32(toPort),
		ReferencedGroupInfo: &awsec2types.ReferencedSecurityGroup{
			GroupId: aws.String("sg-peer"),
			UserId:  aws.String("210987654321"),
			VpcId:   aws.String("vpc-peer"),
		},
		Tags: []awsec2types.Tag{{Key: aws.String("purpose"), Value: aws.String("https")}},
	})

	if rule.ID != "sgr-123" {
		t.Fatalf("ID = %q", rule.ID)
	}
	if rule.ReferencedGroup == nil || rule.ReferencedGroup.GroupID != "sg-peer" {
		t.Fatalf("referenced group = %#v", rule.ReferencedGroup)
	}
	if rule.Tags["purpose"] != "https" {
		t.Fatalf("tag purpose = %q, want https", rule.Tags["purpose"])
	}
}

func TestMapNetworkInterfacePreservesAttachmentAndSecurityGroups(t *testing.T) {
	attachedAt := time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)
	networkInterface := mapNetworkInterface("us-east-1", "123456789012", awsec2types.NetworkInterface{
		Attachment: &awsec2types.NetworkInterfaceAttachment{
			AttachTime:          aws.Time(attachedAt),
			AttachmentId:        aws.String("eni-attach-123"),
			DeleteOnTermination: aws.Bool(true),
			DeviceIndex:         aws.Int32(0),
			InstanceId:          aws.String("i-1234567890abcdef0"),
			InstanceOwnerId:     aws.String("123456789012"),
			Status:              awsec2types.AttachmentStatusAttached,
		},
		AvailabilityZone:   aws.String("us-east-1a"),
		Description:        aws.String("Primary network interface"),
		Groups:             []awsec2types.GroupIdentifier{{GroupId: aws.String("sg-123"), GroupName: aws.String("api")}},
		InterfaceType:      awsec2types.NetworkInterfaceTypeInterface,
		MacAddress:         aws.String("02:00:00:00:00:01"),
		NetworkInterfaceId: aws.String("eni-123"),
		OwnerId:            aws.String("123456789012"),
		PrivateDnsName:     aws.String("ip-10-0-1-10.ec2.internal"),
		PrivateIpAddress:   aws.String("10.0.1.10"),
		RequesterManaged:   aws.Bool(false),
		SourceDestCheck:    aws.Bool(true),
		Status:             awsec2types.NetworkInterfaceStatusInUse,
		SubnetId:           aws.String("subnet-123"),
		VpcId:              aws.String("vpc-123"),
		PrivateIpAddresses: []awsec2types.NetworkInterfacePrivateIpAddress{{PrivateIpAddress: aws.String("10.0.1.10"), Primary: aws.Bool(true)}},
		TagSet:             []awsec2types.Tag{{Key: aws.String("service"), Value: aws.String("api")}},
	})

	if networkInterface.Attachment == nil {
		t.Fatalf("Attachment = nil")
	}
	if got := networkInterface.Attachment.AttachedResourceARN; got != "arn:aws:ec2:us-east-1:123456789012:instance/i-1234567890abcdef0" {
		t.Fatalf("attached resource arn = %q", got)
	}
	if got := networkInterface.SecurityGroups[0].ID; got != "sg-123" {
		t.Fatalf("security group id = %q", got)
	}
	if got := networkInterface.PrivateIPAddresses[0].Address; got != "10.0.1.10" {
		t.Fatalf("private IP = %q", got)
	}
}

func TestNetworkInterfaceInputIncludesManagedResourcesAndPagination(t *testing.T) {
	input := networkInterfacesInput()

	if input.IncludeManagedResources == nil || !*input.IncludeManagedResources {
		t.Fatalf("IncludeManagedResources = %#v, want true", input.IncludeManagedResources)
	}
	if input.MaxResults == nil || *input.MaxResults != ec2PageLimit {
		t.Fatalf("MaxResults = %#v, want %d", input.MaxResults, ec2PageLimit)
	}
}
