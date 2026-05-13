package ec2

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits EC2 VPC, subnet, security group, security group rule, ENI, and
// topology relationship facts for one claimed account and region.
type Scanner struct {
	Client Client
}

// Scan observes EC2 network topology through the configured client.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("ec2 scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "":
		boundary.ServiceKind = awscloud.ServiceEC2
	case awscloud.ServiceEC2:
	default:
		return nil, fmt.Errorf("ec2 scanner received service_kind %q", boundary.ServiceKind)
	}

	var envelopes []facts.Envelope
	vpcs, err := s.Client.ListVPCs(ctx)
	if err != nil {
		return nil, fmt.Errorf("list EC2 VPCs: %w", err)
	}
	for _, vpc := range vpcs {
		resource, err := awscloud.NewResourceEnvelope(vpcObservation(boundary, vpc))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
	}

	subnets, err := s.Client.ListSubnets(ctx)
	if err != nil {
		return nil, fmt.Errorf("list EC2 subnets: %w", err)
	}
	for _, subnet := range subnets {
		subnetEnvelopes, err := subnetEnvelopes(boundary, subnet)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, subnetEnvelopes...)
	}

	securityGroups, err := s.Client.ListSecurityGroups(ctx)
	if err != nil {
		return nil, fmt.Errorf("list EC2 security groups: %w", err)
	}
	for _, securityGroup := range securityGroups {
		securityGroupEnvelopes, err := securityGroupEnvelopes(boundary, securityGroup)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, securityGroupEnvelopes...)
	}

	rules, err := s.Client.ListSecurityGroupRules(ctx)
	if err != nil {
		return nil, fmt.Errorf("list EC2 security group rules: %w", err)
	}
	for _, rule := range rules {
		ruleEnvelopes, err := securityGroupRuleEnvelopes(boundary, rule)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, ruleEnvelopes...)
	}

	networkInterfaces, err := s.Client.ListNetworkInterfaces(ctx)
	if err != nil {
		return nil, fmt.Errorf("list EC2 network interfaces: %w", err)
	}
	for _, networkInterface := range networkInterfaces {
		networkInterfaceEnvelopes, err := networkInterfaceEnvelopes(boundary, networkInterface)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, networkInterfaceEnvelopes...)
	}
	return envelopes, nil
}

func vpcObservation(boundary awscloud.Boundary, vpc VPC) awscloud.ResourceObservation {
	vpcID := strings.TrimSpace(vpc.ID)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   vpcID,
		ResourceType: awscloud.ResourceTypeEC2VPC,
		Name:         vpcID,
		State:        vpc.State,
		Tags:         vpc.Tags,
		Attributes: map[string]any{
			"cidr_block":                   strings.TrimSpace(vpc.CIDRBlock),
			"dhcp_options_id":              strings.TrimSpace(vpc.DHCPOptionsID),
			"instance_tenancy":             strings.TrimSpace(vpc.InstanceTenancy),
			"ipv4_cidr_block_associations": cidrBlockAssociationMaps(vpc.IPv4CIDRBlocks),
			"ipv6_cidr_block_associations": ipv6CIDRBlockAssociationMaps(vpc.IPv6CIDRBlocks),
			"is_default":                   vpc.IsDefault,
			"owner_id":                     strings.TrimSpace(vpc.OwnerID),
		},
		CorrelationAnchors: []string{vpcID},
		SourceRecordID:     vpcID,
	}
}

func subnetEnvelopes(boundary awscloud.Boundary, subnet Subnet) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(subnetObservation(boundary, subnet))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	if relationship, ok := subnetVPCRelationship(boundary, subnet); ok {
		envelope, err := awscloud.NewRelationshipEnvelope(relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func subnetObservation(boundary awscloud.Boundary, subnet Subnet) awscloud.ResourceObservation {
	subnetID := strings.TrimSpace(subnet.ID)
	subnetARN := strings.TrimSpace(subnet.ARN)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          subnetARN,
		ResourceID:   subnetID,
		ResourceType: awscloud.ResourceTypeEC2Subnet,
		Name:         subnetID,
		State:        subnet.State,
		Tags:         subnet.Tags,
		Attributes: map[string]any{
			"assign_ipv6_address_on_creation": subnet.AssignIPv6AddressOnCreate,
			"availability_zone":               strings.TrimSpace(subnet.AvailabilityZone),
			"availability_zone_id":            strings.TrimSpace(subnet.AvailabilityZoneID),
			"available_ip_address_count":      subnet.AvailableIPAddressCount,
			"cidr_block":                      strings.TrimSpace(subnet.CIDRBlock),
			"default_for_az":                  subnet.DefaultForAZ,
			"ipv6_cidr_block_associations":    cidrBlockAssociationMaps(subnet.IPv6CIDRBlocks),
			"ipv6_native":                     subnet.IPv6Native,
			"map_public_ip_on_launch":         subnet.MapPublicIPOnLaunch,
			"outpost_arn":                     strings.TrimSpace(subnet.OutpostARN),
			"owner_id":                        strings.TrimSpace(subnet.OwnerID),
			"vpc_id":                          strings.TrimSpace(subnet.VPCID),
		},
		CorrelationAnchors: []string{subnetARN, subnetID},
		SourceRecordID:     subnetID,
	}
}

func securityGroupEnvelopes(boundary awscloud.Boundary, group SecurityGroup) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(securityGroupObservation(boundary, group))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	if relationship, ok := securityGroupVPCRelationship(boundary, group); ok {
		envelope, err := awscloud.NewRelationshipEnvelope(relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func securityGroupObservation(boundary awscloud.Boundary, group SecurityGroup) awscloud.ResourceObservation {
	groupID := strings.TrimSpace(group.ID)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   groupID,
		ResourceType: awscloud.ResourceTypeEC2SecurityGroup,
		Name:         group.Name,
		Tags:         group.Tags,
		Attributes: map[string]any{
			"description": strings.TrimSpace(group.Description),
			"owner_id":    strings.TrimSpace(group.OwnerID),
			"vpc_id":      strings.TrimSpace(group.VPCID),
		},
		CorrelationAnchors: []string{groupID, strings.TrimSpace(group.Name)},
		SourceRecordID:     groupID,
	}
}

func securityGroupRuleEnvelopes(boundary awscloud.Boundary, rule SecurityGroupRule) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(securityGroupRuleObservation(boundary, rule))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	if relationship, ok := securityGroupRuleRelationship(boundary, rule); ok {
		envelope, err := awscloud.NewRelationshipEnvelope(relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func securityGroupRuleObservation(boundary awscloud.Boundary, rule SecurityGroupRule) awscloud.ResourceObservation {
	ruleID := securityGroupRuleID(rule)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   ruleID,
		ResourceType: awscloud.ResourceTypeEC2SecurityGroupRule,
		Name:         ruleID,
		Tags:         rule.Tags,
		Attributes: map[string]any{
			"cidr_ipv4":        strings.TrimSpace(rule.CIDRIPv4),
			"cidr_ipv6":        strings.TrimSpace(rule.CIDRIPv6),
			"description":      strings.TrimSpace(rule.Description),
			"direction":        securityGroupRuleDirection(rule),
			"from_port":        int32Value(rule.FromPort),
			"group_id":         strings.TrimSpace(rule.GroupID),
			"group_owner_id":   strings.TrimSpace(rule.GroupOwnerID),
			"ip_protocol":      strings.TrimSpace(rule.Protocol),
			"is_egress":        rule.IsEgress,
			"prefix_list_id":   strings.TrimSpace(rule.PrefixListID),
			"referenced_group": referencedSecurityGroupMap(rule.ReferencedGroup),
			"to_port":          int32Value(rule.ToPort),
		},
		CorrelationAnchors: []string{ruleID, strings.TrimSpace(rule.GroupID)},
		SourceRecordID:     ruleID,
	}
}

func networkInterfaceEnvelopes(boundary awscloud.Boundary, networkInterface NetworkInterface) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(networkInterfaceObservation(boundary, networkInterface))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	for _, observation := range networkInterfaceRelationships(boundary, networkInterface) {
		relationship, err := awscloud.NewRelationshipEnvelope(observation)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, relationship)
	}
	return envelopes, nil
}

func networkInterfaceObservation(boundary awscloud.Boundary, networkInterface NetworkInterface) awscloud.ResourceObservation {
	networkInterfaceID := strings.TrimSpace(networkInterface.ID)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   networkInterfaceID,
		ResourceType: awscloud.ResourceTypeEC2NetworkInterface,
		Name:         networkInterfaceID,
		State:        networkInterface.Status,
		Tags:         networkInterface.Tags,
		Attributes: map[string]any{
			"attachment":                 attachmentMap(networkInterface.Attachment),
			"availability_zone":          strings.TrimSpace(networkInterface.AvailabilityZone),
			"description":                strings.TrimSpace(networkInterface.Description),
			"interface_type":             strings.TrimSpace(networkInterface.InterfaceType),
			"ipv6_addresses":             cloneStrings(networkInterface.IPv6Addresses),
			"mac_address":                strings.TrimSpace(networkInterface.MacAddress),
			"owner_id":                   strings.TrimSpace(networkInterface.OwnerID),
			"primary_private_ip_address": strings.TrimSpace(networkInterface.PrivateIPAddress),
			"private_dns_name":           strings.TrimSpace(networkInterface.PrivateDNSName),
			"private_ip_addresses":       privateIPAddressMaps(networkInterface.PrivateIPAddresses),
			"requester_id":               strings.TrimSpace(networkInterface.RequesterID),
			"requester_managed":          networkInterface.RequesterManaged,
			"security_groups":            securityGroupRefMaps(networkInterface.SecurityGroups),
			"source_dest_check":          networkInterface.SourceDestCheck,
			"subnet_id":                  strings.TrimSpace(networkInterface.SubnetID),
			"vpc_id":                     strings.TrimSpace(networkInterface.VPCID),
		},
		CorrelationAnchors: []string{networkInterfaceID},
		SourceRecordID:     networkInterfaceID,
	}
}

func securityGroupRuleID(rule SecurityGroupRule) string {
	if id := strings.TrimSpace(rule.ID); id != "" {
		return id
	}
	return "security-group-rule:" + facts.StableID("EC2SecurityGroupRule", map[string]any{
		"cidr_ipv4":      strings.TrimSpace(rule.CIDRIPv4),
		"cidr_ipv6":      strings.TrimSpace(rule.CIDRIPv6),
		"from_port":      int32Value(rule.FromPort),
		"group_id":       strings.TrimSpace(rule.GroupID),
		"is_egress":      rule.IsEgress,
		"prefix_list_id": strings.TrimSpace(rule.PrefixListID),
		"protocol":       strings.TrimSpace(rule.Protocol),
		"to_port":        int32Value(rule.ToPort),
	})
}

func securityGroupRuleDirection(rule SecurityGroupRule) string {
	if rule.IsEgress {
		return "egress"
	}
	return "ingress"
}

func int32Value(value *int32) any {
	if value == nil {
		return nil
	}
	return *value
}

func timeOrNil(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC()
}
