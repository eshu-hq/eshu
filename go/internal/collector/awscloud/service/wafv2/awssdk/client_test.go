// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awswafv2 "github.com/aws/aws-sdk-go-v2/service/wafv2"
	awswafv2types "github.com/aws/aws-sdk-go-v2/service/wafv2/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

func TestClientListWebACLsExtractsRefsAndManagedRulesNotStatementBodies(t *testing.T) {
	ruleGroupARN := "arn:aws:wafv2:us-east-1:123456789012:regional/rulegroup/custom/rg1"
	ipSetARN := "arn:aws:wafv2:us-east-1:123456789012:regional/ipset/blocklist/ip1"
	regexSetARN := "arn:aws:wafv2:us-east-1:123456789012:regional/regexpatternset/badpaths/rx1"
	webACLARN := "arn:aws:wafv2:us-east-1:123456789012:regional/webacl/edge/abc"
	albARN := "arn:aws:elasticloadbalancing:us-east-1:123456789012:loadbalancer/app/web/1234"

	fake := &fakeWAFv2API{
		webACLSummaries: []awswafv2types.WebACLSummary{{
			ARN:  aws.String(webACLARN),
			Id:   aws.String("abc"),
			Name: aws.String("edge"),
		}},
		webACL: &awswafv2types.WebACL{
			ARN:           aws.String(webACLARN),
			Id:            aws.String("abc"),
			Name:          aws.String("edge"),
			Capacity:      500,
			DefaultAction: &awswafv2types.DefaultAction{Allow: &awswafv2types.AllowAction{}},
			Rules: []awswafv2types.Rule{
				{
					Name: aws.String("group-ref"),
					Statement: &awswafv2types.Statement{
						RuleGroupReferenceStatement: &awswafv2types.RuleGroupReferenceStatement{
							ARN: aws.String(ruleGroupARN),
						},
					},
				},
				{
					Name: aws.String("managed"),
					Statement: &awswafv2types.Statement{
						ManagedRuleGroupStatement: &awswafv2types.ManagedRuleGroupStatement{
							VendorName: aws.String("AWS"),
							Name:       aws.String("AWSManagedRulesCommonRuleSet"),
							Version:    aws.String("Version_1.0"),
						},
					},
				},
				{
					// Nested references inside AND/OR/NOT must still be found,
					// but the byte-match search string must never be persisted.
					Name: aws.String("nested"),
					Statement: &awswafv2types.Statement{
						AndStatement: &awswafv2types.AndStatement{
							Statements: []awswafv2types.Statement{
								{IPSetReferenceStatement: &awswafv2types.IPSetReferenceStatement{ARN: aws.String(ipSetARN)}},
								{NotStatement: &awswafv2types.NotStatement{
									Statement: &awswafv2types.Statement{
										RegexPatternSetReferenceStatement: &awswafv2types.RegexPatternSetReferenceStatement{ARN: aws.String(regexSetARN)},
									},
								}},
								{ByteMatchStatement: &awswafv2types.ByteMatchStatement{
									SearchString: []byte("secret-threat-signature"),
								}},
							},
						},
					},
				},
			},
		},
		resourcesForWebACL: map[string][]string{
			string(awswafv2types.ResourceTypeApplicationLoadBalancer): {albARN},
		},
		webACLTags: map[string]string{"Environment": "prod"},
	}
	adapter := newTestClient(fake, awscloud.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: awscloud.ServiceWAFv2})

	webACLs, err := adapter.ListWebACLs(context.Background())
	if err != nil {
		t.Fatalf("ListWebACLs() error = %v, want nil", err)
	}
	if got, want := len(webACLs), 1; got != want {
		t.Fatalf("len(webACLs) = %d, want %d", got, want)
	}
	webACL := webACLs[0]
	if got, want := webACL.Scope, string(awswafv2types.ScopeRegional); got != want {
		t.Fatalf("Scope = %q, want %q", got, want)
	}
	if got, want := webACL.RuleCount, 3; got != want {
		t.Fatalf("RuleCount = %d, want %d", got, want)
	}
	if got, want := webACL.DefaultAction, "Allow"; got != want {
		t.Fatalf("DefaultAction = %q, want %q", got, want)
	}
	assertContains(t, "rule group refs", webACL.RuleGroupRefARNs, ruleGroupARN)
	assertContains(t, "ip set refs", webACL.IPSetRefARNs, ipSetARN)
	assertContains(t, "regex set refs", webACL.RegexSetRefARNs, regexSetARN)
	if len(webACL.ManagedRuleSetRefs) != 1 ||
		webACL.ManagedRuleSetRefs[0].VendorName != "AWS" ||
		webACL.ManagedRuleSetRefs[0].Name != "AWSManagedRulesCommonRuleSet" {
		t.Fatalf("ManagedRuleSetRefs = %#v, want one AWS common rule set ref", webACL.ManagedRuleSetRefs)
	}
	if len(webACL.ProtectedResources) != 1 || webACL.ProtectedResources[0].ARN != albARN {
		t.Fatalf("ProtectedResources = %#v, want ALB association", webACL.ProtectedResources)
	}
	if webACL.Tags["Environment"] != "prod" {
		t.Fatalf("Tags = %#v, want Environment=prod", webACL.Tags)
	}
}

func TestClientListIPSetsReturnsCountNotAddresses(t *testing.T) {
	ipSetARN := "arn:aws:wafv2:us-east-1:123456789012:regional/ipset/blocklist/ip1"
	fake := &fakeWAFv2API{
		ipSetSummaries: []awswafv2types.IPSetSummary{{
			ARN:  aws.String(ipSetARN),
			Id:   aws.String("ip1"),
			Name: aws.String("blocklist"),
		}},
		ipSet: &awswafv2types.IPSet{
			ARN:              aws.String(ipSetARN),
			Id:               aws.String("ip1"),
			Name:             aws.String("blocklist"),
			IPAddressVersion: awswafv2types.IPAddressVersionIpv4,
			Addresses:        []string{"10.0.0.0/8", "192.168.1.1/32", "172.16.0.0/12"},
		},
	}
	adapter := newTestClient(fake, awscloud.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: awscloud.ServiceWAFv2})

	ipSets, err := adapter.ListIPSets(context.Background())
	if err != nil {
		t.Fatalf("ListIPSets() error = %v, want nil", err)
	}
	if got, want := len(ipSets), 1; got != want {
		t.Fatalf("len(ipSets) = %d, want %d", got, want)
	}
	if got, want := ipSets[0].AddressCount, 3; got != want {
		t.Fatalf("AddressCount = %d, want %d", got, want)
	}
	if got, want := ipSets[0].IPVersion, string(awswafv2types.IPAddressVersionIpv4); got != want {
		t.Fatalf("IPVersion = %q, want %q", got, want)
	}
}

func TestClientListRegexPatternSetsReturnsCountNotBodies(t *testing.T) {
	regexSetARN := "arn:aws:wafv2:us-east-1:123456789012:regional/regexpatternset/badpaths/rx1"
	fake := &fakeWAFv2API{
		regexSummaries: []awswafv2types.RegexPatternSetSummary{{
			ARN:  aws.String(regexSetARN),
			Id:   aws.String("rx1"),
			Name: aws.String("badpaths"),
		}},
		regexSet: &awswafv2types.RegexPatternSet{
			ARN:  aws.String(regexSetARN),
			Id:   aws.String("rx1"),
			Name: aws.String("badpaths"),
			RegularExpressionList: []awswafv2types.Regex{
				{RegexString: aws.String("(?i)/admin")},
				{RegexString: aws.String("\\.\\./")},
			},
		},
	}
	adapter := newTestClient(fake, awscloud.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: awscloud.ServiceWAFv2})

	regexSets, err := adapter.ListRegexPatternSets(context.Background())
	if err != nil {
		t.Fatalf("ListRegexPatternSets() error = %v, want nil", err)
	}
	if got, want := len(regexSets), 1; got != want {
		t.Fatalf("len(regexSets) = %d, want %d", got, want)
	}
	if got, want := regexSets[0].PatternCount, 2; got != want {
		t.Fatalf("PatternCount = %d, want %d", got, want)
	}
}

func TestClientListRuleGroupsReturnsCustomerMetadata(t *testing.T) {
	ruleGroupARN := "arn:aws:wafv2:us-east-1:123456789012:regional/rulegroup/custom/rg1"
	fake := &fakeWAFv2API{
		ruleGroupSummaries: []awswafv2types.RuleGroupSummary{{
			ARN:  aws.String(ruleGroupARN),
			Id:   aws.String("rg1"),
			Name: aws.String("custom"),
		}},
		ruleGroup: &awswafv2types.RuleGroup{
			ARN:      aws.String(ruleGroupARN),
			Id:       aws.String("rg1"),
			Name:     aws.String("custom"),
			Capacity: aws.Int64(200),
			Rules: []awswafv2types.Rule{
				{Name: aws.String("r1"), Statement: &awswafv2types.Statement{
					ByteMatchStatement: &awswafv2types.ByteMatchStatement{SearchString: []byte("never-persist")},
				}},
			},
		},
	}
	adapter := newTestClient(fake, awscloud.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: awscloud.ServiceWAFv2})

	ruleGroups, err := adapter.ListRuleGroups(context.Background())
	if err != nil {
		t.Fatalf("ListRuleGroups() error = %v, want nil", err)
	}
	if got, want := len(ruleGroups), 1; got != want {
		t.Fatalf("len(ruleGroups) = %d, want %d", got, want)
	}
	if got, want := ruleGroups[0].RuleCount, 1; got != want {
		t.Fatalf("RuleCount = %d, want %d", got, want)
	}
	if got, want := ruleGroups[0].Capacity, int64(200); got != want {
		t.Fatalf("Capacity = %d, want %d", got, want)
	}
}

func TestClientUsesCloudFrontScopeForGlobalBoundary(t *testing.T) {
	fake := &fakeWAFv2API{}
	adapter := newTestClient(fake, awscloud.Boundary{AccountID: "123456789012", Region: "aws-global", ServiceKind: awscloud.ServiceWAFv2})

	if _, err := adapter.ListWebACLs(context.Background()); err != nil {
		t.Fatalf("ListWebACLs() error = %v, want nil", err)
	}
	if got, want := fake.lastWebACLScope, awswafv2types.ScopeCloudfront; got != want {
		t.Fatalf("ListWebACLs scope = %q, want %q", got, want)
	}
}

func TestClientUsesRegionalScopeForRegionalBoundary(t *testing.T) {
	fake := &fakeWAFv2API{}
	adapter := newTestClient(fake, awscloud.Boundary{AccountID: "123456789012", Region: "us-west-2", ServiceKind: awscloud.ServiceWAFv2})

	if _, err := adapter.ListIPSets(context.Background()); err != nil {
		t.Fatalf("ListIPSets() error = %v, want nil", err)
	}
	if got, want := fake.lastIPSetScope, awswafv2types.ScopeRegional; got != want {
		t.Fatalf("ListIPSets scope = %q, want %q", got, want)
	}
}

// TestScopeForBoundarySelectsScopeFromCanonicalRegionLabel pins the scope
// selection contract: only the canonical global region label (and an empty
// region, matching the rest of the AWS collector) selects the CLOUDFRONT scope.
// Every other region label, including arbitrary tokens, selects REGIONAL so a
// regional boundary can never be silently routed to the global control plane.
func TestScopeForBoundarySelectsScopeFromCanonicalRegionLabel(t *testing.T) {
	cases := []struct {
		region string
		want   awswafv2types.Scope
	}{
		{region: "aws-global", want: awswafv2types.ScopeCloudfront},
		{region: "  aws-global  ", want: awswafv2types.ScopeCloudfront},
		{region: "", want: awswafv2types.ScopeCloudfront},
		{region: "us-east-1", want: awswafv2types.ScopeRegional},
		{region: "us-west-2", want: awswafv2types.ScopeRegional},
		{region: "eu-central-1", want: awswafv2types.ScopeRegional},
		// Non-canonical tokens must not be treated as the global label.
		{region: "global", want: awswafv2types.ScopeRegional},
		{region: "n", want: awswafv2types.ScopeRegional},
	}
	for _, tc := range cases {
		t.Run(tc.region, func(t *testing.T) {
			got := scopeForBoundary(awscloud.Boundary{Region: tc.region})
			if got != tc.want {
				t.Fatalf("scopeForBoundary(%q) = %q, want %q", tc.region, got, tc.want)
			}
		})
	}
}

// TestClientListIPSetsFollowsNextMarker proves the adapter's explicit marker
// loop fetches every page. WAFv2 list APIs are not standard paginators, so a
// dropped marker would silently truncate results. The fake advertises a marker
// on page one and returns the remainder on page two.
func TestClientListIPSetsFollowsNextMarker(t *testing.T) {
	page1ARN := "arn:aws:wafv2:us-east-1:123456789012:regional/ipset/blocklist/ip1"
	page2ARN := "arn:aws:wafv2:us-east-1:123456789012:regional/ipset/allowlist/ip2"
	fake := &fakeWAFv2API{
		ipSetSummaries: []awswafv2types.IPSetSummary{{
			ARN:  aws.String(page1ARN),
			Id:   aws.String("ip1"),
			Name: aws.String("blocklist"),
		}},
		ipSetSecondPage: []awswafv2types.IPSetSummary{{
			ARN:  aws.String(page2ARN),
			Id:   aws.String("ip2"),
			Name: aws.String("allowlist"),
		}},
		ipSet: &awswafv2types.IPSet{IPAddressVersion: awswafv2types.IPAddressVersionIpv4},
	}
	adapter := newTestClient(fake, awscloud.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: awscloud.ServiceWAFv2})

	ipSets, err := adapter.ListIPSets(context.Background())
	if err != nil {
		t.Fatalf("ListIPSets() error = %v, want nil", err)
	}
	if got, want := len(ipSets), 2; got != want {
		t.Fatalf("len(ipSets) = %d, want %d (marker loop must fetch both pages)", got, want)
	}
	if got, want := fake.ipSetListCalls, 2; got != want {
		t.Fatalf("ListIPSets calls = %d, want %d", got, want)
	}
	if got, want := fake.ipSetPageMarkers[1], "page-2"; got != want {
		t.Fatalf("second page marker = %q, want %q", got, want)
	}
}

func assertContains(t *testing.T, label string, values []string, want string) {
	t.Helper()
	for _, value := range values {
		if value == want {
			return
		}
	}
	t.Fatalf("%s = %#v, want to contain %q", label, values, want)
}

var _ apiClient = (*awswafv2.Client)(nil)
