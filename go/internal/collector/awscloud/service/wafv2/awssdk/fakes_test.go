// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	awswafv2 "github.com/aws/aws-sdk-go-v2/service/wafv2"
	awswafv2types "github.com/aws/aws-sdk-go-v2/service/wafv2/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// newTestClient builds a Client around a fake apiClient without touching the
// AWS SDK constructor. It mirrors the boundary-driven scope selection the
// production NewClient performs.
func newTestClient(api apiClient, boundary awscloud.Boundary) *Client {
	return &Client{
		client:   api,
		boundary: boundary,
		scope:    scopeForBoundary(boundary),
	}
}

// fakeWAFv2API is a read-only WAFv2 API double. It returns the configured
// summaries and detail records and records the scope of each list call so
// tests can assert REGIONAL vs CLOUDFRONT routing.
type fakeWAFv2API struct {
	webACLSummaries []awswafv2types.WebACLSummary
	webACL          *awswafv2types.WebACL
	lastWebACLScope awswafv2types.Scope

	ruleGroupSummaries []awswafv2types.RuleGroupSummary
	ruleGroup          *awswafv2types.RuleGroup

	ipSetSummaries []awswafv2types.IPSetSummary
	ipSet          *awswafv2types.IPSet
	lastIPSetScope awswafv2types.Scope
	// ipSetSecondPage, when set, is returned only after the first ListIPSets
	// call advertises a NextMarker. It exercises the explicit marker loop the
	// adapter runs because WAFv2 list APIs are not standard paginators.
	ipSetSecondPage  []awswafv2types.IPSetSummary
	ipSetListCalls   int
	ipSetPageMarkers []string

	regexSummaries []awswafv2types.RegexPatternSetSummary
	regexSet       *awswafv2types.RegexPatternSet

	resourcesForWebACL map[string][]string
	webACLTags         map[string]string
}

func (f *fakeWAFv2API) ListWebACLs(_ context.Context, input *awswafv2.ListWebACLsInput, _ ...func(*awswafv2.Options)) (*awswafv2.ListWebACLsOutput, error) {
	f.lastWebACLScope = input.Scope
	return &awswafv2.ListWebACLsOutput{WebACLs: f.webACLSummaries}, nil
}

func (f *fakeWAFv2API) GetWebACL(context.Context, *awswafv2.GetWebACLInput, ...func(*awswafv2.Options)) (*awswafv2.GetWebACLOutput, error) {
	return &awswafv2.GetWebACLOutput{WebACL: f.webACL}, nil
}

func (f *fakeWAFv2API) ListResourcesForWebACL(_ context.Context, input *awswafv2.ListResourcesForWebACLInput, _ ...func(*awswafv2.Options)) (*awswafv2.ListResourcesForWebACLOutput, error) {
	return &awswafv2.ListResourcesForWebACLOutput{
		ResourceArns: f.resourcesForWebACL[string(input.ResourceType)],
	}, nil
}

func (f *fakeWAFv2API) ListRuleGroups(context.Context, *awswafv2.ListRuleGroupsInput, ...func(*awswafv2.Options)) (*awswafv2.ListRuleGroupsOutput, error) {
	return &awswafv2.ListRuleGroupsOutput{RuleGroups: f.ruleGroupSummaries}, nil
}

func (f *fakeWAFv2API) GetRuleGroup(context.Context, *awswafv2.GetRuleGroupInput, ...func(*awswafv2.Options)) (*awswafv2.GetRuleGroupOutput, error) {
	return &awswafv2.GetRuleGroupOutput{RuleGroup: f.ruleGroup}, nil
}

func (f *fakeWAFv2API) ListIPSets(_ context.Context, input *awswafv2.ListIPSetsInput, _ ...func(*awswafv2.Options)) (*awswafv2.ListIPSetsOutput, error) {
	f.lastIPSetScope = input.Scope
	f.ipSetListCalls++
	f.ipSetPageMarkers = append(f.ipSetPageMarkers, aws.ToString(input.NextMarker))
	if len(f.ipSetSecondPage) > 0 && input.NextMarker == nil {
		// First page advertises a marker so the adapter must request page two.
		return &awswafv2.ListIPSetsOutput{
			IPSets:     f.ipSetSummaries,
			NextMarker: aws.String("page-2"),
		}, nil
	}
	if len(f.ipSetSecondPage) > 0 {
		return &awswafv2.ListIPSetsOutput{IPSets: f.ipSetSecondPage}, nil
	}
	return &awswafv2.ListIPSetsOutput{IPSets: f.ipSetSummaries}, nil
}

func (f *fakeWAFv2API) GetIPSet(context.Context, *awswafv2.GetIPSetInput, ...func(*awswafv2.Options)) (*awswafv2.GetIPSetOutput, error) {
	return &awswafv2.GetIPSetOutput{IPSet: f.ipSet}, nil
}

func (f *fakeWAFv2API) ListRegexPatternSets(context.Context, *awswafv2.ListRegexPatternSetsInput, ...func(*awswafv2.Options)) (*awswafv2.ListRegexPatternSetsOutput, error) {
	return &awswafv2.ListRegexPatternSetsOutput{RegexPatternSets: f.regexSummaries}, nil
}

func (f *fakeWAFv2API) GetRegexPatternSet(context.Context, *awswafv2.GetRegexPatternSetInput, ...func(*awswafv2.Options)) (*awswafv2.GetRegexPatternSetOutput, error) {
	return &awswafv2.GetRegexPatternSetOutput{RegexPatternSet: f.regexSet}, nil
}

func (f *fakeWAFv2API) ListTagsForResource(context.Context, *awswafv2.ListTagsForResourceInput, ...func(*awswafv2.Options)) (*awswafv2.ListTagsForResourceOutput, error) {
	if len(f.webACLTags) == 0 {
		return &awswafv2.ListTagsForResourceOutput{}, nil
	}
	tagList := make([]awswafv2types.Tag, 0, len(f.webACLTags))
	for key, value := range f.webACLTags {
		tagList = append(tagList, awswafv2types.Tag{Key: aws.String(key), Value: aws.String(value)})
	}
	return &awswafv2.ListTagsForResourceOutput{
		TagInfoForResource: &awswafv2types.TagInfoForResource{TagList: tagList},
	}, nil
}

var _ apiClient = (*fakeWAFv2API)(nil)
