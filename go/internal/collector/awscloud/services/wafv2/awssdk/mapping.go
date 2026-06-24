// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awswafv2 "github.com/aws/aws-sdk-go-v2/service/wafv2"
	awswafv2types "github.com/aws/aws-sdk-go-v2/service/wafv2/types"

	wafv2service "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/wafv2"
)

// ListRuleGroups returns customer-owned rule group metadata. The Scope=CUSTOM
// listing excludes AWS-managed and marketplace rule groups, which surface as
// managed rule set references on web ACLs instead.
func (c *Client) ListRuleGroups(ctx context.Context) ([]wafv2service.RuleGroup, error) {
	var ruleGroups []wafv2service.RuleGroup
	var marker *string
	for {
		var page *awswafv2.ListRuleGroupsOutput
		err := c.recordAPICall(ctx, "ListRuleGroups", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListRuleGroups(callCtx, &awswafv2.ListRuleGroupsInput{
				Scope:      c.scope,
				Limit:      aws.Int32(listLimit),
				NextMarker: marker,
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
		if marker = nextMarker(page.NextMarker); marker == nil {
			return ruleGroups, nil
		}
	}
}

func (c *Client) ruleGroupMetadata(ctx context.Context, summary awswafv2types.RuleGroupSummary) (wafv2service.RuleGroup, error) {
	var output *awswafv2.GetRuleGroupOutput
	err := c.recordAPICall(ctx, "GetRuleGroup", func(callCtx context.Context) error {
		var err error
		output, err = c.client.GetRuleGroup(callCtx, &awswafv2.GetRuleGroupInput{
			Id:    summary.Id,
			Name:  summary.Name,
			Scope: c.scope,
		})
		return err
	})
	if err != nil {
		return wafv2service.RuleGroup{}, err
	}
	ruleGroup := mapRuleGroup(string(c.scope), summary, output)
	tags, err := c.listTags(ctx, aws.ToString(summary.ARN))
	if err != nil {
		return wafv2service.RuleGroup{}, err
	}
	ruleGroup.Tags = tags
	return ruleGroup, nil
}

// ListIPSets returns IP set metadata with the address count only. The address
// list is fetched to count it and is then discarded; it is never returned.
func (c *Client) ListIPSets(ctx context.Context) ([]wafv2service.IPSet, error) {
	var ipSets []wafv2service.IPSet
	var marker *string
	for {
		var page *awswafv2.ListIPSetsOutput
		err := c.recordAPICall(ctx, "ListIPSets", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListIPSets(callCtx, &awswafv2.ListIPSetsInput{
				Scope:      c.scope,
				Limit:      aws.Int32(listLimit),
				NextMarker: marker,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return ipSets, nil
		}
		for _, summary := range page.IPSets {
			ipSet, err := c.ipSetMetadata(ctx, summary)
			if err != nil {
				return nil, err
			}
			ipSets = append(ipSets, ipSet)
		}
		if marker = nextMarker(page.NextMarker); marker == nil {
			return ipSets, nil
		}
	}
}

func (c *Client) ipSetMetadata(ctx context.Context, summary awswafv2types.IPSetSummary) (wafv2service.IPSet, error) {
	var output *awswafv2.GetIPSetOutput
	err := c.recordAPICall(ctx, "GetIPSet", func(callCtx context.Context) error {
		var err error
		output, err = c.client.GetIPSet(callCtx, &awswafv2.GetIPSetInput{
			Id:    summary.Id,
			Name:  summary.Name,
			Scope: c.scope,
		})
		return err
	})
	if err != nil {
		return wafv2service.IPSet{}, err
	}
	ipSet := mapIPSet(string(c.scope), summary, output)
	tags, err := c.listTags(ctx, aws.ToString(summary.ARN))
	if err != nil {
		return wafv2service.IPSet{}, err
	}
	ipSet.Tags = tags
	return ipSet, nil
}

// ListRegexPatternSets returns regex pattern set metadata with the pattern
// count only. The regex bodies are fetched to count them and then discarded;
// they are never returned.
func (c *Client) ListRegexPatternSets(ctx context.Context) ([]wafv2service.RegexPatternSet, error) {
	var regexSets []wafv2service.RegexPatternSet
	var marker *string
	for {
		var page *awswafv2.ListRegexPatternSetsOutput
		err := c.recordAPICall(ctx, "ListRegexPatternSets", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListRegexPatternSets(callCtx, &awswafv2.ListRegexPatternSetsInput{
				Scope:      c.scope,
				Limit:      aws.Int32(listLimit),
				NextMarker: marker,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return regexSets, nil
		}
		for _, summary := range page.RegexPatternSets {
			regexSet, err := c.regexPatternSetMetadata(ctx, summary)
			if err != nil {
				return nil, err
			}
			regexSets = append(regexSets, regexSet)
		}
		if marker = nextMarker(page.NextMarker); marker == nil {
			return regexSets, nil
		}
	}
}

func (c *Client) regexPatternSetMetadata(ctx context.Context, summary awswafv2types.RegexPatternSetSummary) (wafv2service.RegexPatternSet, error) {
	var output *awswafv2.GetRegexPatternSetOutput
	err := c.recordAPICall(ctx, "GetRegexPatternSet", func(callCtx context.Context) error {
		var err error
		output, err = c.client.GetRegexPatternSet(callCtx, &awswafv2.GetRegexPatternSetInput{
			Id:    summary.Id,
			Name:  summary.Name,
			Scope: c.scope,
		})
		return err
	})
	if err != nil {
		return wafv2service.RegexPatternSet{}, err
	}
	regexSet := mapRegexPatternSet(string(c.scope), summary, output)
	tags, err := c.listTags(ctx, aws.ToString(summary.ARN))
	if err != nil {
		return wafv2service.RegexPatternSet{}, err
	}
	regexSet.Tags = tags
	return regexSet, nil
}

func mapWebACL(scope string, summary awswafv2types.WebACLSummary, output *awswafv2.GetWebACLOutput) wafv2service.WebACL {
	webACL := wafv2service.WebACL{
		ARN:         aws.ToString(summary.ARN),
		ID:          aws.ToString(summary.Id),
		Name:        aws.ToString(summary.Name),
		Description: aws.ToString(summary.Description),
		Scope:       scope,
	}
	if output == nil || output.WebACL == nil {
		return webACL
	}
	detail := output.WebACL
	webACL.Capacity = detail.Capacity
	webACL.ManagedByFirewall = detail.ManagedByFirewallManager
	webACL.RuleCount = len(detail.Rules)
	webACL.DefaultAction = defaultActionString(detail.DefaultAction)
	refs := walkRuleReferences(detail.Rules)
	webACL.RuleGroupRefARNs = refs.ruleGroupARNs
	webACL.IPSetRefARNs = refs.ipSetARNs
	webACL.RegexSetRefARNs = refs.regexSetARNs
	webACL.ManagedRuleSetRefs = refs.managedRefs
	return webACL
}

func mapRuleGroup(scope string, summary awswafv2types.RuleGroupSummary, output *awswafv2.GetRuleGroupOutput) wafv2service.RuleGroup {
	ruleGroup := wafv2service.RuleGroup{
		ARN:         aws.ToString(summary.ARN),
		ID:          aws.ToString(summary.Id),
		Name:        aws.ToString(summary.Name),
		Description: aws.ToString(summary.Description),
		Scope:       scope,
	}
	if output == nil || output.RuleGroup == nil {
		return ruleGroup
	}
	ruleGroup.Capacity = aws.ToInt64(output.RuleGroup.Capacity)
	ruleGroup.RuleCount = len(output.RuleGroup.Rules)
	return ruleGroup
}

func mapIPSet(scope string, summary awswafv2types.IPSetSummary, output *awswafv2.GetIPSetOutput) wafv2service.IPSet {
	ipSet := wafv2service.IPSet{
		ARN:         aws.ToString(summary.ARN),
		ID:          aws.ToString(summary.Id),
		Name:        aws.ToString(summary.Name),
		Description: aws.ToString(summary.Description),
		Scope:       scope,
	}
	if output == nil || output.IPSet == nil {
		return ipSet
	}
	ipSet.IPVersion = string(output.IPSet.IPAddressVersion)
	// Count the addresses only. The address list itself is never copied into
	// scanner-owned state because it commonly contains private CIDR and
	// threat-intel data.
	ipSet.AddressCount = len(output.IPSet.Addresses)
	return ipSet
}

func mapRegexPatternSet(scope string, summary awswafv2types.RegexPatternSetSummary, output *awswafv2.GetRegexPatternSetOutput) wafv2service.RegexPatternSet {
	regexSet := wafv2service.RegexPatternSet{
		ARN:         aws.ToString(summary.ARN),
		ID:          aws.ToString(summary.Id),
		Name:        aws.ToString(summary.Name),
		Description: aws.ToString(summary.Description),
		Scope:       scope,
	}
	if output == nil || output.RegexPatternSet == nil {
		return regexSet
	}
	// Count the patterns only. The regex bodies are never copied into
	// scanner-owned state because they encode customer-detection rules.
	regexSet.PatternCount = len(output.RegexPatternSet.RegularExpressionList)
	return regexSet
}

func defaultActionString(action *awswafv2types.DefaultAction) string {
	if action == nil {
		return ""
	}
	switch {
	case action.Allow != nil:
		return "Allow"
	case action.Block != nil:
		return "Block"
	default:
		return ""
	}
}

func mapTags(tags []awswafv2types.Tag) map[string]string {
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
