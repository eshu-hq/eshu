// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package route53resolver

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
)

// uniqueSubnetCount counts the distinct, non-empty subnet IDs placed under an
// endpoint. The SDK adapter populates SubnetIDs with one entry per endpoint IP
// address, so multiple IPs in the same subnet repeat that subnet's ID. Counting
// the deduplicated set keeps subnet_count equal to the endpoint->subnet edge
// cardinality emitted by endpointRelationships, which dedupes the same way.
func uniqueSubnetCount(subnetIDs []string) int {
	seen := make(map[string]struct{}, len(subnetIDs))
	for _, subnet := range subnetIDs {
		subnetID := strings.TrimSpace(subnet)
		if subnetID == "" {
			continue
		}
		seen[subnetID] = struct{}{}
	}
	return len(seen)
}

func endpointObservation(boundary aws.Boundary, endpoint ResolverEndpoint) aws.ResourceObservation {
	id := strings.TrimSpace(endpoint.ID)
	return aws.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   id,
		ARN:          strings.TrimSpace(endpoint.ARN),
		ResourceType: aws.ResourceTypeRoute53ResolverEndpoint,
		Name:         endpoint.Name,
		State:        endpoint.Status,
		Tags:         endpoint.Tags,
		Attributes: map[string]any{
			"direction":        strings.TrimSpace(endpoint.Direction),
			"ip_address_count": endpoint.IPAddressCount,
			"host_vpc_id":      strings.TrimSpace(endpoint.HostVPCID),
			"subnet_count":     uniqueSubnetCount(endpoint.SubnetIDs),
		},
		CorrelationAnchors: []string{id, strings.TrimSpace(endpoint.ARN)},
		SourceRecordID:     id,
	}
}

func ruleObservation(boundary aws.Boundary, rule ResolverRule) aws.ResourceObservation {
	id := strings.TrimSpace(rule.ID)
	return aws.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   id,
		ARN:          strings.TrimSpace(rule.ARN),
		ResourceType: aws.ResourceTypeRoute53ResolverRule,
		Name:         rule.Name,
		State:        rule.Status,
		Tags:         rule.Tags,
		Attributes: map[string]any{
			"domain_name":          strings.TrimSpace(rule.DomainName),
			"rule_type":            strings.TrimSpace(rule.RuleType),
			"share_status":         strings.TrimSpace(rule.ShareStatus),
			"resolver_endpoint_id": strings.TrimSpace(rule.ResolverEndpointID),
		},
		CorrelationAnchors: []string{id, strings.TrimSpace(rule.ARN)},
		SourceRecordID:     id,
	}
}

func ruleAssociationObservation(
	boundary aws.Boundary,
	association ResolverRuleAssociation,
) aws.ResourceObservation {
	id := strings.TrimSpace(association.ID)
	return aws.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   id,
		ResourceType: aws.ResourceTypeRoute53ResolverRuleAssociation,
		Name:         association.Name,
		State:        association.Status,
		Attributes: map[string]any{
			"resolver_rule_id": strings.TrimSpace(association.ResolverRuleID),
			"vpc_id":           strings.TrimSpace(association.VPCID),
		},
		CorrelationAnchors: []string{id},
		SourceRecordID:     id,
	}
}

func firewallRuleGroupObservation(
	boundary aws.Boundary,
	group FirewallRuleGroup,
) aws.ResourceObservation {
	id := strings.TrimSpace(group.ID)
	return aws.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   id,
		ARN:          strings.TrimSpace(group.ARN),
		ResourceType: aws.ResourceTypeRoute53ResolverFirewallRuleGroup,
		Name:         group.Name,
		State:        group.Status,
		Tags:         group.Tags,
		Attributes: map[string]any{
			"rule_count":   group.RuleCount,
			"share_status": strings.TrimSpace(group.ShareStatus),
			"owner_id":     strings.TrimSpace(group.OwnerID),
		},
		CorrelationAnchors: []string{id, strings.TrimSpace(group.ARN)},
		SourceRecordID:     id,
	}
}

func firewallDomainListObservation(
	boundary aws.Boundary,
	list FirewallDomainList,
) aws.ResourceObservation {
	id := strings.TrimSpace(list.ID)
	return aws.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   id,
		ARN:          strings.TrimSpace(list.ARN),
		ResourceType: aws.ResourceTypeRoute53ResolverFirewallDomainList,
		Name:         list.Name,
		State:        list.Status,
		Tags:         list.Tags,
		Attributes: map[string]any{
			"domain_count":       list.DomainCount,
			"managed_owner_name": strings.TrimSpace(list.ManagedOwnerName),
		},
		CorrelationAnchors: []string{id, strings.TrimSpace(list.ARN)},
		SourceRecordID:     id,
	}
}

func firewallRuleGroupAssociationObservation(
	boundary aws.Boundary,
	association FirewallRuleGroupAssociation,
) aws.ResourceObservation {
	id := strings.TrimSpace(association.ID)
	return aws.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   id,
		ARN:          strings.TrimSpace(association.ARN),
		ResourceType: aws.ResourceTypeRoute53ResolverFirewallRuleGroupAssociation,
		Name:         association.Name,
		State:        association.Status,
		Attributes: map[string]any{
			"firewall_rule_group_id": strings.TrimSpace(association.FirewallRuleGroupID),
			"vpc_id":                 strings.TrimSpace(association.VPCID),
			"priority":               association.Priority,
			"mutation_protection":    strings.TrimSpace(association.MutationProtection),
		},
		CorrelationAnchors: []string{id, strings.TrimSpace(association.ARN)},
		SourceRecordID:     id,
	}
}

func queryLogConfigObservation(
	boundary aws.Boundary,
	config QueryLogConfig,
) aws.ResourceObservation {
	id := strings.TrimSpace(config.ID)
	return aws.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   id,
		ARN:          strings.TrimSpace(config.ARN),
		ResourceType: aws.ResourceTypeRoute53ResolverQueryLogConfig,
		Name:         config.Name,
		State:        config.Status,
		Tags:         config.Tags,
		Attributes: map[string]any{
			"destination_arn": strings.TrimSpace(config.DestinationARN),
			"share_status":    strings.TrimSpace(config.ShareStatus),
			"owner_id":        strings.TrimSpace(config.OwnerID),
		},
		CorrelationAnchors: []string{id, strings.TrimSpace(config.ARN)},
		SourceRecordID:     id,
	}
}
