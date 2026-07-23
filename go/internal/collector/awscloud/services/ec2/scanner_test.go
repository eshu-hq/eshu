// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ec2

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestScannerEmitsNetworkTopologyWithoutInstanceFacts(t *testing.T) {
	fromPort := int32(443)
	toPort := int32(443)
	client := fakeClient{
		vpcs: []VPC{{
			ID:            "vpc-123",
			OwnerID:       "123456789012",
			State:         "available",
			CIDRBlock:     "10.0.0.0/16",
			DHCPOptionsID: "dopt-123",
			IsDefault:     false,
			Tags:          map[string]string{"env": "prod"},
		}},
		subnets: []Subnet{{
			ARN:                       "arn:aws:ec2:us-east-1:123456789012:subnet/subnet-123",
			ID:                        "subnet-123",
			VPCID:                     "vpc-123",
			OwnerID:                   "123456789012",
			State:                     "available",
			CIDRBlock:                 "10.0.1.0/24",
			AvailabilityZone:          "us-east-1a",
			AvailabilityZoneID:        "use1-az1",
			AvailableIPAddressCount:   200,
			DefaultForAZ:              false,
			MapPublicIPOnLaunch:       true,
			AssignIPv6AddressOnCreate: true,
			Tags:                      map[string]string{"tier": "app"},
		}},
		securityGroups: []SecurityGroup{{
			ID:          "sg-123",
			Name:        "api",
			Description: "api ingress",
			VPCID:       "vpc-123",
			OwnerID:     "123456789012",
			Tags:        map[string]string{"service": "api"},
		}},
		securityGroupRules: []SecurityGroupRule{{
			ID:           "sgr-123",
			GroupID:      "sg-123",
			GroupOwnerID: "123456789012",
			IsEgress:     false,
			Protocol:     "tcp",
			FromPort:     &fromPort,
			ToPort:       &toPort,
			CIDRIPv4:     "0.0.0.0/0",
			Description:  "public https",
			Tags:         map[string]string{"purpose": "https"},
		}},
		networkInterfaces: []NetworkInterface{{
			ID:                 "eni-123",
			VPCID:              "vpc-123",
			SubnetID:           "subnet-123",
			OwnerID:            "123456789012",
			Status:             "in-use",
			InterfaceType:      "interface",
			Description:        "Primary network interface",
			AvailabilityZone:   "us-east-1a",
			MacAddress:         "02:00:00:00:00:01",
			PrivateDNSName:     "ip-10-0-1-10.ec2.internal",
			PrivateIPAddress:   "10.0.1.10",
			RequesterManaged:   false,
			SourceDestCheck:    true,
			SecurityGroups:     []SecurityGroupRef{{ID: "sg-123", Name: "api"}},
			PrivateIPAddresses: []PrivateIPAddress{{Address: "10.0.1.10", Primary: true}},
			Attachment: &NetworkInterfaceAttachment{
				ID:                   "eni-attach-123",
				InstanceID:           "i-1234567890abcdef0",
				InstanceOwnerID:      "123456789012",
				AttachedResourceARN:  "arn:aws:ec2:us-east-1:123456789012:instance/i-1234567890abcdef0",
				AttachedResourceType: "aws_ec2_instance",
				Status:               "attached",
				DeleteOnTermination:  true,
				DeviceIndex:          0,
			},
			Tags: map[string]string{"service": "api"},
		}},
	}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}

	counts := factKindCounts(envelopes)
	if counts[facts.AWSResourceFactKind] != 5 {
		t.Fatalf("aws_resource count = %d, want 5", counts[facts.AWSResourceFactKind])
	}
	if counts[facts.AWSRelationshipFactKind] != 7 {
		t.Fatalf("aws_relationship count = %d, want 7", counts[facts.AWSRelationshipFactKind])
	}
	if counts[facts.AWSSecurityGroupRuleFactKind] != 1 {
		t.Fatalf("aws_security_group_rule count = %d, want 1", counts[facts.AWSSecurityGroupRuleFactKind])
	}
	assertNoResourceType(t, envelopes, "aws_ec2_instance")

	ruleFact := assertSecurityGroupRuleFact(t, envelopes)
	if got := ruleFact.Payload["group_id"]; got != "sg-123" {
		t.Fatalf("security_group_rule group_id = %#v, want sg-123", got)
	}
	if got := ruleFact.Payload["direction"]; got != awscloud.SecurityGroupRuleDirectionIngress {
		t.Fatalf("security_group_rule direction = %#v, want ingress", got)
	}
	if got := ruleFact.Payload["source_kind"]; got != awscloud.SecurityGroupRuleSourceCIDRIPv4 {
		t.Fatalf("security_group_rule source_kind = %#v, want cidr_ipv4", got)
	}
	if got := ruleFact.Payload["source_value"]; got != "0.0.0.0/0" {
		t.Fatalf("security_group_rule source_value = %#v, want 0.0.0.0/0", got)
	}
	if got, _ := ruleFact.Payload["is_internet"].(bool); !got {
		t.Fatalf("security_group_rule is_internet = %#v, want true", ruleFact.Payload["is_internet"])
	}

	vpc := assertResourceType(t, envelopes, awscloud.ResourceTypeEC2VPC)
	assertAttribute(t, vpc, "cidr_block", "10.0.0.0/16")
	subnet := assertResourceType(t, envelopes, awscloud.ResourceTypeEC2Subnet)
	assertAttribute(t, subnet, "vpc_id", "vpc-123")
	rule := assertResourceType(t, envelopes, awscloud.ResourceTypeEC2SecurityGroupRule)
	assertAttribute(t, rule, "cidr_ipv4", "0.0.0.0/0")
	eni := assertResourceType(t, envelopes, awscloud.ResourceTypeEC2NetworkInterface)
	assertAttachment(t, eni)

	assertRelationship(t, envelopes, awscloud.RelationshipEC2SubnetInVPC)
	assertRelationship(t, envelopes, awscloud.RelationshipEC2SecurityGroupInVPC)
	assertRelationship(t, envelopes, awscloud.RelationshipEC2SecurityGroupHasRule)
	assertRelationship(t, envelopes, awscloud.RelationshipEC2NetworkInterfaceInSubnet)
	assertRelationship(t, envelopes, awscloud.RelationshipEC2NetworkInterfaceInVPC)
	assertRelationship(t, envelopes, awscloud.RelationshipEC2NetworkInterfaceUsesSecurityGroup)
	attached := assertRelationship(t, envelopes, awscloud.RelationshipEC2NetworkInterfaceAttachedToResource)
	if got := attached.Payload["target_resource_id"]; got != "i-1234567890abcdef0" {
		t.Fatalf("attached target_resource_id = %#v", got)
	}
	if got := attached.Payload["target_arn"]; got != "arn:aws:ec2:us-east-1:123456789012:instance/i-1234567890abcdef0" {
		t.Fatalf("attached target_arn = %#v", got)
	}
}

func TestScannerEmitsInstancePostureAndIdentityFacts(t *testing.T) {
	imdsv2 := true
	hopLimit := int32(1)
	userData := true
	client := fakeClient{
		instances: []Instance{{
			ID:                      "i-1234567890abcdef0",
			ARN:                     "arn:aws:ec2:us-east-1:123456789012:instance/i-1234567890abcdef0",
			State:                   "running",
			ImageID:                 "ami-0000000000000000a",
			IMDSv2Required:          &imdsv2,
			HTTPEndpoint:            "enabled",
			HTTPPutResponseHopLimit: &hopLimit,
			UserDataPresent:         &userData,
			DetailedMonitoring:      true,
			EBSOptimized:            true,
			PublicIPAssociated:      true,
			PublicIPAddress:         "203.0.113.10",
			InstanceProfileARN:      "arn:aws:iam::123456789012:instance-profile/app",
			Tenancy:                 "default",
			NitroEnclaveEnabled:     true,
			BlockDevices: []BlockDevice{{
				DeviceName:          "/dev/xvda",
				VolumeID:            "vol-0abc",
				DeleteOnTermination: true,
				Status:              "attached",
			}},
		}},
	}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}

	counts := factKindCounts(envelopes)
	if counts[facts.EC2InstancePostureFactKind] != 1 {
		t.Fatalf("ec2_instance_posture count = %d, want 1", counts[facts.EC2InstancePostureFactKind])
	}
	if counts[facts.AWSResourceFactKind] != 1 {
		t.Fatalf("aws_resource count = %d, want 1 (#5448 identity fact)", counts[facts.AWSResourceFactKind])
	}
	if counts[facts.AWSRelationshipFactKind] != 1 {
		t.Fatalf("aws_relationship count = %d, want 1 (#5448 instance->AMI relationship)", counts[facts.AWSRelationshipFactKind])
	}

	identity := assertResourceType(t, envelopes, awscloud.ResourceTypeEC2Instance)
	if got := identity.Payload["resource_id"]; got != "i-1234567890abcdef0" {
		t.Fatalf("identity resource_id = %#v, want i-1234567890abcdef0", got)
	}
	assertAttribute(t, identity, "ami_id", "ami-0000000000000000a")

	amiRelationship := assertRelationship(t, envelopes, awscloud.RelationshipEC2InstanceUsesAMI)
	if got := amiRelationship.Payload["source_resource_id"]; got != "i-1234567890abcdef0" {
		t.Fatalf("ami relationship source_resource_id = %#v", got)
	}
	if got := amiRelationship.Payload["target_resource_id"]; got != "ami-0000000000000000a" {
		t.Fatalf("ami relationship target_resource_id = %#v", got)
	}
	if got := amiRelationship.Payload["target_type"]; got != awscloud.ResourceTypeEC2AMI {
		t.Fatalf("ami relationship target_type = %#v, want %s", got, awscloud.ResourceTypeEC2AMI)
	}

	posture := assertInstancePostureFact(t, envelopes)
	if got := posture.Payload["instance_id"]; got != "i-1234567890abcdef0" {
		t.Fatalf("posture instance_id = %#v, want i-1234567890abcdef0", got)
	}
	if got, _ := posture.Payload["imds_v2_required"].(bool); !got {
		t.Fatalf("posture imds_v2_required = %#v, want true", posture.Payload["imds_v2_required"])
	}
	if got, _ := posture.Payload["user_data_present"].(bool); !got {
		t.Fatalf("posture user_data_present = %#v, want true", posture.Payload["user_data_present"])
	}
	if got := posture.Payload["instance_profile_arn"]; got != "arn:aws:iam::123456789012:instance-profile/app" {
		t.Fatalf("posture instance_profile_arn = %#v", got)
	}
	if got, _ := posture.Payload["service_kind"].(string); got != awscloud.ServiceEC2 {
		t.Fatalf("posture service_kind = %#v, want ec2", got)
	}
	if _, exists := posture.Payload["relationship_type"]; exists {
		t.Fatalf("posture fact carried a relationship_type; PR1 posture is facts-only")
	}
	for _, forbidden := range []string{"user_data", "user_data_content", "console_output", "environment"} {
		if _, exists := posture.Payload[forbidden]; exists {
			t.Fatalf("%s persisted on posture fact; EC2 posture must stay metadata-only", forbidden)
		}
	}
}

func TestScannerEmitsIdentityWithoutAMIRelationshipWhenImageIDBlank(t *testing.T) {
	client := fakeClient{
		instances: []Instance{{
			ID:    "i-1234567890abcdef1",
			ARN:   "arn:aws:ec2:us-east-1:123456789012:instance/i-1234567890abcdef1",
			State: "running",
			// ImageID intentionally blank: the identity fact still emits (an
			// instance always has an identity), but no AMI relationship is
			// possible without a target id.
		}},
	}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}

	counts := factKindCounts(envelopes)
	if counts[facts.AWSResourceFactKind] != 1 {
		t.Fatalf("aws_resource count = %d, want 1", counts[facts.AWSResourceFactKind])
	}
	if counts[facts.AWSRelationshipFactKind] != 0 {
		t.Fatalf("aws_relationship count = %d, want 0 (no AMI id to relate)", counts[facts.AWSRelationshipFactKind])
	}
	identity := assertResourceType(t, envelopes, awscloud.ResourceTypeEC2Instance)
	assertAttribute(t, identity, "ami_id", "")
}

func assertInstancePostureFact(t *testing.T, envelopes []facts.Envelope) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind == facts.EC2InstancePostureFactKind {
			return envelope
		}
	}
	t.Fatalf("missing %q fact in %#v", facts.EC2InstancePostureFactKind, envelopes)
	return facts.Envelope{}
}

func TestScannerRejectsMismatchedServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = awscloud.ServiceECS
	_, err := (Scanner{Client: fakeClient{}}).Scan(context.Background(), boundary)
	if err == nil {
		t.Fatalf("Scan() error = nil, want service kind mismatch")
	}
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceEC2,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:ec2:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        42,
		ObservedAt:          time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC),
	}
}

type fakeClient struct {
	vpcs               []VPC
	subnets            []Subnet
	securityGroups     []SecurityGroup
	securityGroupRules []SecurityGroupRule
	networkInterfaces  []NetworkInterface
	instances          []Instance
	volumes            []Volume
}

func (c fakeClient) ListVPCs(context.Context) ([]VPC, error) {
	return c.vpcs, nil
}

func (c fakeClient) ListSubnets(context.Context) ([]Subnet, error) {
	return c.subnets, nil
}

func (c fakeClient) ListSecurityGroups(context.Context) ([]SecurityGroup, error) {
	return c.securityGroups, nil
}

func (c fakeClient) ListSecurityGroupRules(context.Context) ([]SecurityGroupRule, error) {
	return c.securityGroupRules, nil
}

func (c fakeClient) ListNetworkInterfaces(context.Context) ([]NetworkInterface, error) {
	return c.networkInterfaces, nil
}

func (c fakeClient) ListInstances(context.Context) ([]Instance, error) {
	return c.instances, nil
}

func (c fakeClient) ListVolumes(context.Context) ([]Volume, error) {
	return c.volumes, nil
}

func factKindCounts(envelopes []facts.Envelope) map[string]int {
	counts := make(map[string]int)
	for _, envelope := range envelopes {
		counts[envelope.FactKind]++
	}
	return counts
}

func assertResourceType(t *testing.T, envelopes []facts.Envelope, resourceType string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		if got, _ := envelope.Payload["resource_type"].(string); got == resourceType {
			return envelope
		}
	}
	t.Fatalf("missing resource_type %q in %#v", resourceType, envelopes)
	return facts.Envelope{}
}

func assertNoResourceType(t *testing.T, envelopes []facts.Envelope, resourceType string) {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		if got, _ := envelope.Payload["resource_type"].(string); got == resourceType {
			t.Fatalf("unexpected resource_type %q", resourceType)
		}
	}
}

func assertResourceID(t *testing.T, envelopes []facts.Envelope, resourceID string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		if got, _ := envelope.Payload["resource_id"].(string); got == resourceID {
			return envelope
		}
	}
	t.Fatalf("missing resource_id %q in %#v", resourceID, envelopes)
	return facts.Envelope{}
}

func assertSecurityGroupRuleFact(t *testing.T, envelopes []facts.Envelope) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind == facts.AWSSecurityGroupRuleFactKind {
			return envelope
		}
	}
	t.Fatalf("missing %q fact in %#v", facts.AWSSecurityGroupRuleFactKind, envelopes)
	return facts.Envelope{}
}

func assertRelationship(t *testing.T, envelopes []facts.Envelope, relationshipType string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got == relationshipType {
			return envelope
		}
	}
	t.Fatalf("missing relationship_type %q in %#v", relationshipType, envelopes)
	return facts.Envelope{}
}

func assertAttribute(t *testing.T, envelope facts.Envelope, key string, want any) {
	t.Helper()
	attributes := attributesOf(t, envelope)
	if attributes[key] != want {
		t.Fatalf("attribute %s = %#v, want %#v", key, attributes[key], want)
	}
}

func attributesOf(t *testing.T, envelope facts.Envelope) map[string]any {
	t.Helper()
	attributes, ok := envelope.Payload["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("attributes = %#v, want map", envelope.Payload["attributes"])
	}
	return attributes
}

func assertAttachment(t *testing.T, envelope facts.Envelope) {
	t.Helper()
	attributes, ok := envelope.Payload["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("attributes = %#v, want map", envelope.Payload["attributes"])
	}
	attachment, ok := attributes["attachment"].(map[string]any)
	if !ok {
		t.Fatalf("attachment = %#v, want map", attributes["attachment"])
	}
	if got, _ := attachment["instance_id"].(string); got != "i-1234567890abcdef0" {
		t.Fatalf("attachment.instance_id = %q", got)
	}
	if got, _ := attachment["attached_resource_arn"].(string); !strings.Contains(got, ":instance/") {
		t.Fatalf("attachment.attached_resource_arn = %q, want instance ARN", got)
	}
}
