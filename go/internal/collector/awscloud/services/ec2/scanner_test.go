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
	assertNoResourceType(t, envelopes, "aws_ec2_instance")

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
	attributes, ok := envelope.Payload["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("attributes = %#v, want map", envelope.Payload["attributes"])
	}
	if attributes[key] != want {
		t.Fatalf("attribute %s = %#v, want %#v", key, attributes[key], want)
	}
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
