// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsnetfw "github.com/aws/aws-sdk-go-v2/service/networkfirewall"
	awsnetfwtypes "github.com/aws/aws-sdk-go-v2/service/networkfirewall/types"

	netfwservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/networkfirewall"
)

// ListFirewallPolicies returns firewall policy metadata, default-action names,
// and the rule group and TLS inspection configuration ARN references resolved
// from DescribeFirewallPolicy. It never returns the full policy rule body
// beyond the action names and reference ARNs the policy reports.
func (c *Client) ListFirewallPolicies(ctx context.Context) ([]netfwservice.FirewallPolicy, error) {
	var policies []netfwservice.FirewallPolicy
	var token *string
	for {
		var page *awsnetfw.ListFirewallPoliciesOutput
		err := c.recordAPICall(ctx, "ListFirewallPolicies", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListFirewallPolicies(callCtx, &awsnetfw.ListFirewallPoliciesInput{
				MaxResults: aws.Int32(listLimit),
				NextToken:  token,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return policies, nil
		}
		for _, summary := range page.FirewallPolicies {
			policy, err := c.firewallPolicyMetadata(ctx, summary)
			if err != nil {
				return nil, err
			}
			policies = append(policies, policy)
		}
		if token = nextToken(page.NextToken); token == nil {
			return policies, nil
		}
	}
}

func (c *Client) firewallPolicyMetadata(ctx context.Context, summary awsnetfwtypes.FirewallPolicyMetadata) (netfwservice.FirewallPolicy, error) {
	var output *awsnetfw.DescribeFirewallPolicyOutput
	err := c.recordAPICall(ctx, "DescribeFirewallPolicy", func(callCtx context.Context) error {
		var err error
		output, err = c.client.DescribeFirewallPolicy(callCtx, &awsnetfw.DescribeFirewallPolicyInput{
			FirewallPolicyArn: summary.Arn,
		})
		return err
	})
	if err != nil {
		return netfwservice.FirewallPolicy{}, err
	}
	return mapFirewallPolicy(output), nil
}

// ListRuleGroups returns rule group metadata (type and capacity) resolved from
// DescribeRuleGroupMetadata. DescribeRuleGroupMetadata never returns the rule
// source, so the Suricata signature bodies cannot leak into facts. The Scope
// defaults to ACCOUNT so only customer-owned rule groups are listed; managed
// rule groups stay with the vendor.
func (c *Client) ListRuleGroups(ctx context.Context) ([]netfwservice.RuleGroup, error) {
	var ruleGroups []netfwservice.RuleGroup
	var token *string
	for {
		var page *awsnetfw.ListRuleGroupsOutput
		err := c.recordAPICall(ctx, "ListRuleGroups", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListRuleGroups(callCtx, &awsnetfw.ListRuleGroupsInput{
				MaxResults: aws.Int32(listLimit),
				NextToken:  token,
				Scope:      awsnetfwtypes.ResourceManagedStatusAccount,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return ruleGroups, nil
		}
		for _, summary := range page.RuleGroups {
			ruleGroup, err := c.ruleGroupMetadata(ctx, summary)
			if err != nil {
				return nil, err
			}
			ruleGroups = append(ruleGroups, ruleGroup)
		}
		if token = nextToken(page.NextToken); token == nil {
			return ruleGroups, nil
		}
	}
}

func (c *Client) ruleGroupMetadata(ctx context.Context, summary awsnetfwtypes.RuleGroupMetadata) (netfwservice.RuleGroup, error) {
	var output *awsnetfw.DescribeRuleGroupMetadataOutput
	err := c.recordAPICall(ctx, "DescribeRuleGroupMetadata", func(callCtx context.Context) error {
		var err error
		output, err = c.client.DescribeRuleGroupMetadata(callCtx, &awsnetfw.DescribeRuleGroupMetadataInput{
			RuleGroupArn: summary.Arn,
		})
		return err
	})
	if err != nil {
		return netfwservice.RuleGroup{}, err
	}
	ruleGroup := mapRuleGroup(output)
	tags, err := c.listTags(ctx, ruleGroup.ARN)
	if err != nil {
		return netfwservice.RuleGroup{}, err
	}
	ruleGroup.Tags = tags
	return ruleGroup, nil
}

// ListTLSInspectionConfigurations returns TLS inspection configuration metadata
// resolved from DescribeTLSInspectionConfiguration. Only the response metadata
// is read; certificate bodies and TLS inspection scope rule bodies are never
// copied into scanner-owned state.
func (c *Client) ListTLSInspectionConfigurations(ctx context.Context) ([]netfwservice.TLSInspectionConfiguration, error) {
	var tlsConfigs []netfwservice.TLSInspectionConfiguration
	var token *string
	for {
		var page *awsnetfw.ListTLSInspectionConfigurationsOutput
		err := c.recordAPICall(ctx, "ListTLSInspectionConfigurations", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListTLSInspectionConfigurations(callCtx, &awsnetfw.ListTLSInspectionConfigurationsInput{
				MaxResults: aws.Int32(listLimit),
				NextToken:  token,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return tlsConfigs, nil
		}
		for _, summary := range page.TLSInspectionConfigurations {
			tlsConfig, err := c.tlsInspectionConfigurationMetadata(ctx, summary)
			if err != nil {
				return nil, err
			}
			tlsConfigs = append(tlsConfigs, tlsConfig)
		}
		if token = nextToken(page.NextToken); token == nil {
			return tlsConfigs, nil
		}
	}
}

func (c *Client) tlsInspectionConfigurationMetadata(ctx context.Context, summary awsnetfwtypes.TLSInspectionConfigurationMetadata) (netfwservice.TLSInspectionConfiguration, error) {
	var output *awsnetfw.DescribeTLSInspectionConfigurationOutput
	err := c.recordAPICall(ctx, "DescribeTLSInspectionConfiguration", func(callCtx context.Context) error {
		var err error
		output, err = c.client.DescribeTLSInspectionConfiguration(callCtx, &awsnetfw.DescribeTLSInspectionConfigurationInput{
			TLSInspectionConfigurationArn: summary.Arn,
		})
		return err
	})
	if err != nil {
		return netfwservice.TLSInspectionConfiguration{}, err
	}
	return mapTLSInspectionConfiguration(output), nil
}

func mapFirewall(output *awsnetfw.DescribeFirewallOutput) netfwservice.Firewall {
	if output == nil || output.Firewall == nil {
		return netfwservice.Firewall{}
	}
	detail := output.Firewall
	firewall := netfwservice.Firewall{
		ARN:                            aws.ToString(detail.FirewallArn),
		ID:                             aws.ToString(detail.FirewallId),
		Name:                           aws.ToString(detail.FirewallName),
		Description:                    aws.ToString(detail.Description),
		VPCID:                          aws.ToString(detail.VpcId),
		FirewallPolicyARN:              aws.ToString(detail.FirewallPolicyArn),
		DeleteProtection:               detail.DeleteProtection,
		SubnetChangeProtection:         detail.SubnetChangeProtection,
		FirewallPolicyChangeProtection: detail.FirewallPolicyChangeProtection,
		NumberOfAssociations:           aws.ToInt32(detail.NumberOfAssociations),
		Tags:                           mapTags(detail.Tags),
	}
	for _, mapping := range detail.SubnetMappings {
		if id := strings.TrimSpace(aws.ToString(mapping.SubnetId)); id != "" {
			firewall.SubnetIDs = append(firewall.SubnetIDs, id)
		}
	}
	if status := output.FirewallStatus; status != nil {
		firewall.Status = string(status.Status)
		firewall.ConfigurationSyncState = string(status.ConfigurationSyncStateSummary)
	}
	return firewall
}

// mapFirewallPolicy reads the policy response metadata plus the policy's
// default-action names and rule group / TLS inspection configuration reference
// ARNs. The policy's stateless and stateful rule group references carry only
// ARNs and priorities; no rule body is read. The action lists are AWS-defined
// action keywords (for example aws:forward_to_sfe), not customer rule content.
func mapFirewallPolicy(output *awsnetfw.DescribeFirewallPolicyOutput) netfwservice.FirewallPolicy {
	if output == nil {
		return netfwservice.FirewallPolicy{}
	}
	policy := netfwservice.FirewallPolicy{}
	if response := output.FirewallPolicyResponse; response != nil {
		policy.ARN = aws.ToString(response.FirewallPolicyArn)
		policy.ID = aws.ToString(response.FirewallPolicyId)
		policy.Name = aws.ToString(response.FirewallPolicyName)
		policy.Description = aws.ToString(response.Description)
		policy.Status = string(response.FirewallPolicyStatus)
		policy.NumberOfAssociations = aws.ToInt32(response.NumberOfAssociations)
		policy.ConsumedStatefulRuleCapacity = aws.ToInt32(response.ConsumedStatefulRuleCapacity)
		policy.ConsumedStatelessRuleCapacity = aws.ToInt32(response.ConsumedStatelessRuleCapacity)
		policy.Tags = mapTags(response.Tags)
	}
	if detail := output.FirewallPolicy; detail != nil {
		policy.StatelessDefaultActions = trimStrings(detail.StatelessDefaultActions)
		policy.StatelessFragmentDefaultActions = trimStrings(detail.StatelessFragmentDefaultActions)
		policy.StatefulDefaultActions = trimStrings(detail.StatefulDefaultActions)
		policy.TLSInspectionConfigurationARN = strings.TrimSpace(aws.ToString(detail.TLSInspectionConfigurationArn))
		for _, ref := range detail.StatefulRuleGroupReferences {
			if arn := strings.TrimSpace(aws.ToString(ref.ResourceArn)); arn != "" {
				policy.RuleGroupARNs = append(policy.RuleGroupARNs, arn)
			}
		}
		for _, ref := range detail.StatelessRuleGroupReferences {
			if arn := strings.TrimSpace(aws.ToString(ref.ResourceArn)); arn != "" {
				policy.RuleGroupARNs = append(policy.RuleGroupARNs, arn)
			}
		}
	}
	return policy
}

// mapRuleGroup reads rule group metadata only. The source is
// DescribeRuleGroupMetadataOutput, which carries identity, type, capacity, and
// description but never the rule source (Suricata signature bodies).
func mapRuleGroup(output *awsnetfw.DescribeRuleGroupMetadataOutput) netfwservice.RuleGroup {
	if output == nil {
		return netfwservice.RuleGroup{}
	}
	return netfwservice.RuleGroup{
		ARN:         aws.ToString(output.RuleGroupArn),
		Name:        aws.ToString(output.RuleGroupName),
		Description: aws.ToString(output.Description),
		Type:        string(output.Type),
		Capacity:    aws.ToInt32(output.Capacity),
	}
}

func mapTLSInspectionConfiguration(output *awsnetfw.DescribeTLSInspectionConfigurationOutput) netfwservice.TLSInspectionConfiguration {
	if output == nil || output.TLSInspectionConfigurationResponse == nil {
		return netfwservice.TLSInspectionConfiguration{}
	}
	response := output.TLSInspectionConfigurationResponse
	return netfwservice.TLSInspectionConfiguration{
		ARN:                  aws.ToString(response.TLSInspectionConfigurationArn),
		ID:                   aws.ToString(response.TLSInspectionConfigurationId),
		Name:                 aws.ToString(response.TLSInspectionConfigurationName),
		Description:          aws.ToString(response.Description),
		Status:               string(response.TLSInspectionConfigurationStatus),
		NumberOfAssociations: aws.ToInt32(response.NumberOfAssociations),
		Tags:                 mapTags(response.Tags),
	}
}

func mapTags(tags []awsnetfwtypes.Tag) map[string]string {
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
	if len(output) == 0 {
		return nil
	}
	return output
}

func trimStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	output := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			output = append(output, trimmed)
		}
	}
	if len(output) == 0 {
		return nil
	}
	return output
}
