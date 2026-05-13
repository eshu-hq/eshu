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
