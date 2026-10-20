// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ec2

import (
	"context"
	"time"
)

// Client is the EC2 read surface consumed by Scanner. Runtime adapters should
// translate AWS SDK responses into these scanner-owned network topology types.
type Client interface {
	ListVPCs(context.Context) ([]VPC, error)
	ListSubnets(context.Context) ([]Subnet, error)
	ListSecurityGroups(context.Context) ([]SecurityGroup, error)
	ListSecurityGroupRules(context.Context) ([]SecurityGroupRule, error)
	ListNetworkInterfaces(context.Context) ([]NetworkInterface, error)
	// ListInstances returns instance posture inputs read from the existing
	// DescribeInstances pass. It carries no user-data content: presence only.
	ListInstances(context.Context) ([]Instance, error)
	// ListVolumes returns EBS volume metadata read from one boundary-scoped
	// DescribeVolumes pass. It is not used to fill instance posture inline.
	ListVolumes(context.Context) ([]Volume, error)
}

// Instance is the scanner-owned representation of EC2 instance posture inputs.
// It is the metadata-only projection of one DescribeInstances entry used to
// derive the ec2_instance_posture fact. It never carries user-data content,
// console output, or any other instance payload: UserDataPresent is a presence
// boolean derived at the SDK boundary.
type Instance struct {
	ID           string
	ARN          string
	State        string
	OwnerID      string
	InstanceType string
	SubnetID     string
	VPCID        string

	// ImageID is the AMI (ImageId) the instance was launched from, read from
	// the same DescribeInstances entry as every other field on this struct (no
	// additional AWS API call). It backs the #5448 EC2 instance identity
	// aws_resource fact and the instance->AMI relationship; it is never read
	// by the ec2_instance_posture fact or its CloudResource node
	// materialization.
	ImageID string

	// IMDS settings.
	IMDSv2Required          *bool
	HTTPEndpoint            string
	HTTPPutResponseHopLimit *int32

	// UserDataPresent reports whether user-data is attached. The content is
	// never read. Nil means presence could not be determined.
	UserDataPresent *bool

	DetailedMonitoring  bool
	EBSOptimized        bool
	PublicIPAssociated  bool
	PublicIPAddress     string
	InstanceProfileARN  string
	Tenancy             string
	NitroEnclaveEnabled bool

	BlockDevices []BlockDevice
	Tags         map[string]string
}

// BlockDevice is one instance block-device mapping entry. Per-volume encryption
// is not reported by DescribeInstances; Encrypted stays nil here and reducers
// resolve it from volume evidence.
type BlockDevice struct {
	DeviceName          string
	VolumeID            string
	DeleteOnTermination bool
	Status              string
	Encrypted           *bool
}

// Volume is the scanner-owned representation of one EBS volume returned by
// DescribeVolumes. It carries metadata-only encryption, KMS, attachment, and
// operational shape; it never carries volume contents or snapshot payloads.
type Volume struct {
	ID                       string
	ARN                      string
	State                    string
	AvailabilityZone         string
	AvailabilityZoneID       string
	CreateTime               time.Time
	Encrypted                *bool
	FastRestored             *bool
	IOPS                     *int32
	KMSKeyID                 string
	MultiAttachEnabled       *bool
	OutpostARN               string
	SizeGiB                  *int32
	SnapshotID               string
	SourceVolumeID           string
	SSEType                  string
	ThroughputMiBps          *int32
	VolumeInitializationRate *int32
	VolumeType               string
	Attachments              []VolumeAttachment
	Tags                     map[string]string
}

// VolumeAttachment is one EBS volume attachment entry reported by
// DescribeVolumes. AWS-managed attachment targets are kept as reported metadata
// and are not resolved into workload or service truth by this scanner.
type VolumeAttachment struct {
	AssociatedResource    string
	AttachTime            time.Time
	DeleteOnTermination   bool
	Device                string
	EBSCardIndex          *int32
	InstanceID            string
	InstanceOwningService string
	State                 string
	VolumeID              string
}

// VPC is the scanner-owned representation of an EC2 VPC.
type VPC struct {
	ID              string
	OwnerID         string
	State           string
	CIDRBlock       string
	DHCPOptionsID   string
	InstanceTenancy string
	IsDefault       bool
	IPv4CIDRBlocks  []CIDRBlockAssociation
	IPv6CIDRBlocks  []IPv6CIDRBlockAssociation
	Tags            map[string]string
}

// Subnet is the scanner-owned representation of an EC2 subnet.
type Subnet struct {
	ARN                       string
	ID                        string
	VPCID                     string
	OwnerID                   string
	State                     string
	CIDRBlock                 string
	AvailabilityZone          string
	AvailabilityZoneID        string
	AvailableIPAddressCount   int32
	DefaultForAZ              bool
	MapPublicIPOnLaunch       bool
	AssignIPv6AddressOnCreate bool
	IPv6Native                bool
	OutpostARN                string
	IPv6CIDRBlocks            []CIDRBlockAssociation
	Tags                      map[string]string
}

// SecurityGroup is the scanner-owned representation of an EC2 security group.
type SecurityGroup struct {
	ID          string
	Name        string
	Description string
	VPCID       string
	OwnerID     string
	Tags        map[string]string
}

// SecurityGroupRule is the scanner-owned representation of one EC2 security
// group rule.
type SecurityGroupRule struct {
	ID              string
	GroupID         string
	GroupOwnerID    string
	IsEgress        bool
	Protocol        string
	FromPort        *int32
	ToPort          *int32
	CIDRIPv4        string
	CIDRIPv6        string
	PrefixListID    string
	ReferencedGroup *ReferencedSecurityGroup
	Description     string
	Tags            map[string]string
}

// ReferencedSecurityGroup captures a security-group rule target.
type ReferencedSecurityGroup struct {
	GroupID                string
	UserID                 string
	VPCID                  string
	PeeringStatus          string
	VPCPeeringConnectionID string
}

// NetworkInterface is the scanner-owned representation of EC2 network
// interface metadata.
type NetworkInterface struct {
	ID                 string
	VPCID              string
	SubnetID           string
	OwnerID            string
	Status             string
	InterfaceType      string
	Description        string
	AvailabilityZone   string
	MacAddress         string
	PrivateDNSName     string
	PrivateIPAddress   string
	RequesterID        string
	RequesterManaged   bool
	SourceDestCheck    bool
	SecurityGroups     []SecurityGroupRef
	PrivateIPAddresses []PrivateIPAddress
	IPv6Addresses      []string
	Attachment         *NetworkInterfaceAttachment
	Tags               map[string]string
}

// NetworkInterfaceAttachment captures ENI attachment metadata without
// inventorying the attached resource.
type NetworkInterfaceAttachment struct {
	ID                   string
	InstanceID           string
	InstanceOwnerID      string
	AttachedResourceARN  string
	AttachedResourceType string
	Status               string
	AttachTime           time.Time
	DeleteOnTermination  bool
	DeviceIndex          int32
	NetworkCardIndex     int32
}

// SecurityGroupRef is one security group attached to an ENI.
type SecurityGroupRef struct {
	ID   string
	Name string
}

// PrivateIPAddress captures one private IPv4 address assigned to an ENI.
type PrivateIPAddress struct {
	Address        string
	PrivateDNSName string
	Primary        bool
}

// CIDRBlockAssociation captures an IPv4 CIDR association.
type CIDRBlockAssociation struct {
	AssociationID string
	CIDRBlock     string
	State         string
}

// IPv6CIDRBlockAssociation captures an IPv6 CIDR association.
type IPv6CIDRBlockAssociation struct {
	AssociationID      string
	CIDRBlock          string
	State              string
	IPv6Pool           string
	NetworkBorderGroup string
}
