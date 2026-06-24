// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsr53rtypes "github.com/aws/aws-sdk-go-v2/service/route53resolver/types"

	r53rservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/route53resolver"
)

func mapResolverEndpoint(
	endpoint awsr53rtypes.ResolverEndpoint,
	subnetIDs []string,
	tags map[string]string,
) r53rservice.ResolverEndpoint {
	return r53rservice.ResolverEndpoint{
		ID:             aws.ToString(endpoint.Id),
		ARN:            aws.ToString(endpoint.Arn),
		Name:           aws.ToString(endpoint.Name),
		Direction:      string(endpoint.Direction),
		Status:         string(endpoint.Status),
		IPAddressCount: aws.ToInt32(endpoint.IpAddressCount),
		HostVPCID:      aws.ToString(endpoint.HostVPCId),
		SubnetIDs:      subnetIDs,
		Tags:           tags,
	}
}

func mapResolverRule(rule awsr53rtypes.ResolverRule, tags map[string]string) r53rservice.ResolverRule {
	return r53rservice.ResolverRule{
		ID:                 aws.ToString(rule.Id),
		ARN:                aws.ToString(rule.Arn),
		Name:               aws.ToString(rule.Name),
		DomainName:         aws.ToString(rule.DomainName),
		RuleType:           string(rule.RuleType),
		Status:             string(rule.Status),
		ShareStatus:        string(rule.ShareStatus),
		ResolverEndpointID: aws.ToString(rule.ResolverEndpointId),
		Tags:               tags,
	}
}

func mapResolverRuleAssociation(
	association awsr53rtypes.ResolverRuleAssociation,
) r53rservice.ResolverRuleAssociation {
	return r53rservice.ResolverRuleAssociation{
		ID:             aws.ToString(association.Id),
		Name:           aws.ToString(association.Name),
		ResolverRuleID: aws.ToString(association.ResolverRuleId),
		VPCID:          aws.ToString(association.VPCId),
		Status:         string(association.Status),
	}
}

func mapFirewallRuleGroup(group awsr53rtypes.FirewallRuleGroup) r53rservice.FirewallRuleGroup {
	return r53rservice.FirewallRuleGroup{
		ID:          aws.ToString(group.Id),
		ARN:         aws.ToString(group.Arn),
		Name:        aws.ToString(group.Name),
		RuleCount:   aws.ToInt32(group.RuleCount),
		Status:      string(group.Status),
		ShareStatus: string(group.ShareStatus),
		OwnerID:     aws.ToString(group.OwnerId),
	}
}

func mapFirewallDomainList(list awsr53rtypes.FirewallDomainList) r53rservice.FirewallDomainList {
	return r53rservice.FirewallDomainList{
		ID:               aws.ToString(list.Id),
		ARN:              aws.ToString(list.Arn),
		Name:             aws.ToString(list.Name),
		DomainCount:      aws.ToInt32(list.DomainCount),
		Status:           string(list.Status),
		ManagedOwnerName: aws.ToString(list.ManagedOwnerName),
	}
}

func mapFirewallRuleGroupAssociation(
	association awsr53rtypes.FirewallRuleGroupAssociation,
) r53rservice.FirewallRuleGroupAssociation {
	return r53rservice.FirewallRuleGroupAssociation{
		ID:                  aws.ToString(association.Id),
		ARN:                 aws.ToString(association.Arn),
		Name:                aws.ToString(association.Name),
		FirewallRuleGroupID: aws.ToString(association.FirewallRuleGroupId),
		VPCID:               aws.ToString(association.VpcId),
		Priority:            aws.ToInt32(association.Priority),
		MutationProtection:  string(association.MutationProtection),
		Status:              string(association.Status),
	}
}

func mapQueryLogConfig(config awsr53rtypes.ResolverQueryLogConfig, tags map[string]string) r53rservice.QueryLogConfig {
	return r53rservice.QueryLogConfig{
		ID:             aws.ToString(config.Id),
		ARN:            aws.ToString(config.Arn),
		Name:           aws.ToString(config.Name),
		DestinationARN: aws.ToString(config.DestinationArn),
		Status:         string(config.Status),
		ShareStatus:    string(config.ShareStatus),
		OwnerID:        aws.ToString(config.OwnerId),
		Tags:           tags,
	}
}

func mapTags(tags []awsr53rtypes.Tag) map[string]string {
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
