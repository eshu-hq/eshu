// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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

func TestMapInstanceDerivesMetadataOnlyPosture(t *testing.T) {
	instance := mapInstance("us-east-1", "123456789012", awsec2types.Instance{
		InstanceId:      aws.String("i-1234567890abcdef0"),
		InstanceType:    awsec2types.InstanceTypeM5Large,
		State:           &awsec2types.InstanceState{Name: awsec2types.InstanceStateNameRunning},
		SubnetId:        aws.String("subnet-123"),
		VpcId:           aws.String("vpc-123"),
		EbsOptimized:    aws.Bool(true),
		PublicIpAddress: aws.String("203.0.113.10"),
		Monitoring:      &awsec2types.Monitoring{State: awsec2types.MonitoringStateEnabled},
		MetadataOptions: &awsec2types.InstanceMetadataOptionsResponse{
			HttpTokens:              awsec2types.HttpTokensStateRequired,
			HttpEndpoint:            awsec2types.InstanceMetadataEndpointStateEnabled,
			HttpPutResponseHopLimit: aws.Int32(1),
		},
		IamInstanceProfile: &awsec2types.IamInstanceProfile{
			Arn: aws.String("arn:aws:iam::123456789012:instance-profile/app"),
		},
		Placement:      &awsec2types.Placement{Tenancy: awsec2types.TenancyDefault},
		EnclaveOptions: &awsec2types.EnclaveOptions{Enabled: aws.Bool(true)},
		BlockDeviceMappings: []awsec2types.InstanceBlockDeviceMapping{{
			DeviceName: aws.String("/dev/xvda"),
			Ebs: &awsec2types.EbsInstanceBlockDevice{
				VolumeId:            aws.String("vol-0abc"),
				DeleteOnTermination: aws.Bool(true),
				Status:              awsec2types.AttachmentStatusAttached,
			},
		}},
		Tags: []awsec2types.Tag{{Key: aws.String("env"), Value: aws.String("prod")}},
	})

	if instance.ARN != "arn:aws:ec2:us-east-1:123456789012:instance/i-1234567890abcdef0" {
		t.Fatalf("ARN = %q", instance.ARN)
	}
	if instance.IMDSv2Required == nil || !*instance.IMDSv2Required {
		t.Fatalf("IMDSv2Required = %#v, want true", instance.IMDSv2Required)
	}
	if instance.HTTPEndpoint != "enabled" {
		t.Fatalf("HTTPEndpoint = %q, want enabled", instance.HTTPEndpoint)
	}
	if instance.HTTPPutResponseHopLimit == nil || *instance.HTTPPutResponseHopLimit != 1 {
		t.Fatalf("HTTPPutResponseHopLimit = %#v, want 1", instance.HTTPPutResponseHopLimit)
	}
	if !instance.DetailedMonitoring {
		t.Fatalf("DetailedMonitoring = false, want true")
	}
	if !instance.EBSOptimized {
		t.Fatalf("EBSOptimized = false, want true")
	}
	if !instance.PublicIPAssociated || instance.PublicIPAddress != "203.0.113.10" {
		t.Fatalf("public IP = %v/%q, want true/203.0.113.10", instance.PublicIPAssociated, instance.PublicIPAddress)
	}
	if instance.InstanceProfileARN != "arn:aws:iam::123456789012:instance-profile/app" {
		t.Fatalf("InstanceProfileARN = %q", instance.InstanceProfileARN)
	}
	if instance.Tenancy != "default" {
		t.Fatalf("Tenancy = %q, want default", instance.Tenancy)
	}
	if !instance.NitroEnclaveEnabled {
		t.Fatalf("NitroEnclaveEnabled = false, want true")
	}
	if len(instance.BlockDevices) != 1 || instance.BlockDevices[0].VolumeID != "vol-0abc" {
		t.Fatalf("BlockDevices = %#v", instance.BlockDevices)
	}
	if instance.BlockDevices[0].Encrypted != nil {
		t.Fatalf("BlockDevices[0].Encrypted = %#v, want nil (DescribeInstances does not report it)", instance.BlockDevices[0].Encrypted)
	}
	// DescribeInstances carries no user-data; the mapper must not invent presence.
	if instance.UserDataPresent != nil {
		t.Fatalf("UserDataPresent = %#v, want nil (no per-instance user-data read)", instance.UserDataPresent)
	}
}

func TestMapInstanceDerivesPartitionForGovCloud(t *testing.T) {
	instance := mapInstance("us-gov-west-1", "123456789012", awsec2types.Instance{
		InstanceId: aws.String("i-abc"),
	})
	if instance.ARN != "arn:aws-us-gov:ec2:us-gov-west-1:123456789012:instance/i-abc" {
		t.Fatalf("ARN = %q, want gov partition", instance.ARN)
	}
	if instance.IMDSv2Required != nil {
		t.Fatalf("IMDSv2Required = %#v, want nil when MetadataOptions absent", instance.IMDSv2Required)
	}
}

func TestMapVolumePreservesEncryptionKMSAndAttachments(t *testing.T) {
	createTime := time.Date(2026, 5, 13, 11, 0, 0, 0, time.UTC)
	attachTime := time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)
	volume := mapVolume("us-east-1", "123456789012", awsec2types.Volume{
		AvailabilityZone:         aws.String("us-east-1a"),
		AvailabilityZoneId:       aws.String("use1-az1"),
		CreateTime:               aws.Time(createTime),
		Encrypted:                aws.Bool(true),
		FastRestored:             aws.Bool(false),
		Iops:                     aws.Int32(3000),
		KmsKeyId:                 aws.String("arn:aws:kms:us-east-1:123456789012:key/abcd"),
		MultiAttachEnabled:       aws.Bool(true),
		Size:                     aws.Int32(100),
		SnapshotId:               aws.String("snap-123"),
		Throughput:               aws.Int32(125),
		VolumeId:                 aws.String("vol-0abc"),
		VolumeType:               awsec2types.VolumeTypeGp3,
		State:                    awsec2types.VolumeStateInUse,
		VolumeInitializationRate: aws.Int32(250),
		Attachments: []awsec2types.VolumeAttachment{{
			AttachTime:          aws.Time(attachTime),
			DeleteOnTermination: aws.Bool(true),
			Device:              aws.String("/dev/xvda"),
			EbsCardIndex:        aws.Int32(1),
			InstanceId:          aws.String("i-1234567890abcdef0"),
			State:               awsec2types.VolumeAttachmentStateAttached,
			VolumeId:            aws.String("vol-0abc"),
		}},
		Tags: []awsec2types.Tag{{Key: aws.String("env"), Value: aws.String("prod")}},
	})

	if volume.ID != "vol-0abc" {
		t.Fatalf("ID = %q, want vol-0abc", volume.ID)
	}
	if volume.ARN != "arn:aws:ec2:us-east-1:123456789012:volume/vol-0abc" {
		t.Fatalf("ARN = %q", volume.ARN)
	}
	if volume.Encrypted == nil || !*volume.Encrypted {
		t.Fatalf("Encrypted = %#v, want true", volume.Encrypted)
	}
	if volume.KMSKeyID != "arn:aws:kms:us-east-1:123456789012:key/abcd" {
		t.Fatalf("KMSKeyID = %q", volume.KMSKeyID)
	}
	if volume.MultiAttachEnabled == nil || !*volume.MultiAttachEnabled {
		t.Fatalf("MultiAttachEnabled = %#v, want true", volume.MultiAttachEnabled)
	}
	if volume.VolumeInitializationRate == nil || *volume.VolumeInitializationRate != 250 {
		t.Fatalf("VolumeInitializationRate = %#v, want 250", volume.VolumeInitializationRate)
	}
	if len(volume.Attachments) != 1 || volume.Attachments[0].InstanceID != "i-1234567890abcdef0" {
		t.Fatalf("Attachments = %#v", volume.Attachments)
	}
	if volume.Attachments[0].EBSCardIndex == nil || *volume.Attachments[0].EBSCardIndex != 1 {
		t.Fatalf("EBSCardIndex = %#v, want 1", volume.Attachments[0].EBSCardIndex)
	}
	if volume.Tags["env"] != "prod" {
		t.Fatalf("tag env = %q, want prod", volume.Tags["env"])
	}
}

func TestMapVolumeDerivesPartitionForGovCloud(t *testing.T) {
	volume := mapVolume("us-gov-west-1", "123456789012", awsec2types.Volume{
		VolumeId: aws.String("vol-0abc"),
	})
	if volume.ARN != "arn:aws-us-gov:ec2:us-gov-west-1:123456789012:volume/vol-0abc" {
		t.Fatalf("ARN = %q, want gov partition", volume.ARN)
	}
}

func TestMapVolumePreservesSDKPointersForOptionalScalars(t *testing.T) {
	encrypted := aws.Bool(true)
	iops := aws.Int32(3000)
	ebsCardIndex := aws.Int32(1)

	volume := mapVolume("us-east-1", "123456789012", awsec2types.Volume{
		Encrypted: encrypted,
		Iops:      iops,
		VolumeId:  aws.String("vol-0abc"),
		Attachments: []awsec2types.VolumeAttachment{{
			EbsCardIndex: ebsCardIndex,
		}},
	})

	if volume.Encrypted != encrypted {
		t.Fatalf("Encrypted pointer = %p, want original SDK pointer %p", volume.Encrypted, encrypted)
	}
	if volume.IOPS != iops {
		t.Fatalf("IOPS pointer = %p, want original SDK pointer %p", volume.IOPS, iops)
	}
	if volume.Attachments[0].EBSCardIndex != ebsCardIndex {
		t.Fatalf("EBSCardIndex pointer = %p, want original SDK pointer %p", volume.Attachments[0].EBSCardIndex, ebsCardIndex)
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

// TestMapSubnetPreservesVPCLinkageAndIPv6 proves mapSubnet records the VpcId
// (the RUNS_IN foundation field) and the IPv6 CIDR associations alongside the
// other subnet identity fields. VPCID is the critical cross-resource link used
// by the runtimebind reducer to derive RUNS_IN edges; it must survive the
// mapping without truncation.
func TestMapSubnetPreservesVPCLinkageAndIPv6(t *testing.T) {
	subnet := mapSubnet(awsec2types.Subnet{
		SubnetId:                    aws.String("subnet-abc123"),
		SubnetArn:                   aws.String("arn:aws:ec2:us-east-1:123456789012:subnet/subnet-abc123"),
		VpcId:                       aws.String("vpc-123"),
		OwnerId:                     aws.String("123456789012"),
		State:                       awsec2types.SubnetStateAvailable,
		CidrBlock:                   aws.String("10.0.1.0/24"),
		AvailabilityZone:            aws.String("us-east-1a"),
		AvailabilityZoneId:          aws.String("use1-az1"),
		AvailableIpAddressCount:     aws.Int32(251),
		DefaultForAz:                aws.Bool(false),
		MapPublicIpOnLaunch:         aws.Bool(false),
		AssignIpv6AddressOnCreation: aws.Bool(true),
		Ipv6Native:                  aws.Bool(false),
		OutpostArn:                  aws.String(""),
		Ipv6CidrBlockAssociationSet: []awsec2types.SubnetIpv6CidrBlockAssociation{{
			AssociationId: aws.String("subnet-cidr-assoc-v6-1"),
			Ipv6CidrBlock: aws.String("2001:db8::/64"),
		}},
		Tags: []awsec2types.Tag{{Key: aws.String("tier"), Value: aws.String("private")}},
	})

	if subnet.ID != "subnet-abc123" {
		t.Fatalf("ID = %q, want subnet-abc123", subnet.ID)
	}
	if subnet.VPCID != "vpc-123" {
		t.Fatalf("VPCID = %q, want vpc-123 (cross-resource link for RUNS_IN foundation)", subnet.VPCID)
	}
	if subnet.CIDRBlock != "10.0.1.0/24" {
		t.Fatalf("CIDRBlock = %q, want 10.0.1.0/24", subnet.CIDRBlock)
	}
	if subnet.AvailabilityZone != "us-east-1a" {
		t.Fatalf("AvailabilityZone = %q, want us-east-1a", subnet.AvailabilityZone)
	}
	if got, want := len(subnet.IPv6CIDRBlocks), 1; got != want {
		t.Fatalf("len(IPv6CIDRBlocks) = %d, want %d", got, want)
	}
	if subnet.IPv6CIDRBlocks[0].CIDRBlock != "2001:db8::/64" {
		t.Fatalf("IPv6CIDRBlocks[0].CIDRBlock = %q, want 2001:db8::/64", subnet.IPv6CIDRBlocks[0].CIDRBlock)
	}
	if subnet.Tags["tier"] != "private" {
		t.Fatalf("tag tier = %q, want private", subnet.Tags["tier"])
	}
}

// TestMapSecurityGroupPreservesVPCAndOwner proves mapSecurityGroup records the
// VpcId and OwnerId fields that the runtimebind reducer uses to scope the
// security-group to the correct VPC and account context. The ALLOWS_INGRESS
// edges derived from security-group rules depend on these fields being present.
func TestMapSecurityGroupPreservesVPCAndOwner(t *testing.T) {
	sg := mapSecurityGroup(awsec2types.SecurityGroup{
		GroupId:     aws.String("sg-prod-api"),
		GroupName:   aws.String("prod-api"),
		Description: aws.String("prod API security group"),
		VpcId:       aws.String("vpc-123"),
		OwnerId:     aws.String("123456789012"),
		Tags:        []awsec2types.Tag{{Key: aws.String("service"), Value: aws.String("api")}},
	})

	if sg.ID != "sg-prod-api" {
		t.Fatalf("ID = %q, want sg-prod-api", sg.ID)
	}
	if sg.VPCID != "vpc-123" {
		t.Fatalf("VPCID = %q, want vpc-123", sg.VPCID)
	}
	if sg.OwnerID != "123456789012" {
		t.Fatalf("OwnerID = %q, want 123456789012", sg.OwnerID)
	}
	if sg.Name != "prod-api" {
		t.Fatalf("Name = %q, want prod-api", sg.Name)
	}
	if sg.Tags["service"] != "api" {
		t.Fatalf("tag service = %q, want api", sg.Tags["service"])
	}
}
