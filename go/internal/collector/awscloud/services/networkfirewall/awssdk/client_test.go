// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsnetfw "github.com/aws/aws-sdk-go-v2/service/networkfirewall"
	awsnetfwtypes "github.com/aws/aws-sdk-go-v2/service/networkfirewall/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	netfwservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/networkfirewall"
)

func TestListFirewallsPaginatesAndMaps(t *testing.T) {
	fake := &fakeAPIClient{
		firewallPages: [][]awsnetfwtypes.FirewallMetadata{
			{{FirewallArn: aws.String("arn:fw:1"), FirewallName: aws.String("fw1")}},
			{{FirewallArn: aws.String("arn:fw:2"), FirewallName: aws.String("fw2")}},
		},
		firewalls: map[string]*awsnetfw.DescribeFirewallOutput{
			"arn:fw:1": {
				Firewall: &awsnetfwtypes.Firewall{
					FirewallArn:       aws.String("arn:fw:1"),
					FirewallId:        aws.String("id1"),
					FirewallName:      aws.String("fw1"),
					VpcId:             aws.String("vpc-1"),
					FirewallPolicyArn: aws.String("arn:fp:1"),
					DeleteProtection:  true,
					SubnetMappings: []awsnetfwtypes.SubnetMapping{
						{SubnetId: aws.String("subnet-1")},
						{SubnetId: aws.String("subnet-2")},
					},
					Tags: []awsnetfwtypes.Tag{{Key: aws.String("env"), Value: aws.String("prod")}},
				},
				FirewallStatus: &awsnetfwtypes.FirewallStatus{
					Status:                        awsnetfwtypes.FirewallStatusValueReady,
					ConfigurationSyncStateSummary: awsnetfwtypes.ConfigurationSyncStateInSync,
				},
			},
			"arn:fw:2": {
				Firewall: &awsnetfwtypes.Firewall{
					FirewallArn:  aws.String("arn:fw:2"),
					FirewallName: aws.String("fw2"),
					VpcId:        aws.String("vpc-2"),
				},
			},
		},
	}
	client := newTestClient(fake)

	firewalls, err := client.ListFirewalls(context.Background())
	if err != nil {
		t.Fatalf("ListFirewalls() error = %v", err)
	}
	if len(firewalls) != 2 {
		t.Fatalf("ListFirewalls() returned %d firewalls, want 2", len(firewalls))
	}
	first := firewalls[0]
	if first.VPCID != "vpc-1" || first.FirewallPolicyARN != "arn:fp:1" || !first.DeleteProtection {
		t.Fatalf("firewall 1 mapped incorrectly: %#v", first)
	}
	if len(first.SubnetIDs) != 2 || first.SubnetIDs[0] != "subnet-1" {
		t.Fatalf("firewall 1 subnets = %#v, want [subnet-1 subnet-2]", first.SubnetIDs)
	}
	if first.Status != "READY" || first.ConfigurationSyncState != "IN_SYNC" {
		t.Fatalf("firewall 1 status = %q sync = %q", first.Status, first.ConfigurationSyncState)
	}
	if first.Tags["env"] != "prod" {
		t.Fatalf("firewall 1 tags = %#v, want env=prod", first.Tags)
	}
}

func TestListFirewallPoliciesMapsActionsAndReferences(t *testing.T) {
	fake := &fakeAPIClient{
		policyPages: [][]awsnetfwtypes.FirewallPolicyMetadata{
			{{Arn: aws.String("arn:fp:1"), Name: aws.String("fp1")}},
		},
		policies: map[string]*awsnetfw.DescribeFirewallPolicyOutput{
			"arn:fp:1": {
				FirewallPolicyResponse: &awsnetfwtypes.FirewallPolicyResponse{
					FirewallPolicyArn:    aws.String("arn:fp:1"),
					FirewallPolicyId:     aws.String("fpid1"),
					FirewallPolicyName:   aws.String("fp1"),
					FirewallPolicyStatus: awsnetfwtypes.ResourceStatusActive,
					NumberOfAssociations: aws.Int32(2),
				},
				FirewallPolicy: &awsnetfwtypes.FirewallPolicy{
					StatelessDefaultActions:         []string{"aws:forward_to_sfe"},
					StatelessFragmentDefaultActions: []string{"aws:drop"},
					StatefulDefaultActions:          []string{"aws:drop_strict"},
					TLSInspectionConfigurationArn:   aws.String("arn:tls:1"),
					StatefulRuleGroupReferences: []awsnetfwtypes.StatefulRuleGroupReference{
						{ResourceArn: aws.String("arn:rg:stateful")},
					},
					StatelessRuleGroupReferences: []awsnetfwtypes.StatelessRuleGroupReference{
						{ResourceArn: aws.String("arn:rg:stateless"), Priority: aws.Int32(1)},
					},
				},
			},
		},
	}
	client := newTestClient(fake)

	policies, err := client.ListFirewallPolicies(context.Background())
	if err != nil {
		t.Fatalf("ListFirewallPolicies() error = %v", err)
	}
	if len(policies) != 1 {
		t.Fatalf("ListFirewallPolicies() returned %d, want 1", len(policies))
	}
	policy := policies[0]
	if policy.Status != "ACTIVE" {
		t.Fatalf("policy status = %q, want ACTIVE", policy.Status)
	}
	if len(policy.StatefulDefaultActions) != 1 || policy.StatefulDefaultActions[0] != "aws:drop_strict" {
		t.Fatalf("stateful default actions = %#v", policy.StatefulDefaultActions)
	}
	if policy.TLSInspectionConfigurationARN != "arn:tls:1" {
		t.Fatalf("TLS config ARN = %q", policy.TLSInspectionConfigurationARN)
	}
	wantRefs := map[string]bool{"arn:rg:stateful": true, "arn:rg:stateless": true}
	if len(policy.RuleGroupARNs) != 2 {
		t.Fatalf("rule group ARNs = %#v, want 2", policy.RuleGroupARNs)
	}
	for _, arn := range policy.RuleGroupARNs {
		if !wantRefs[arn] {
			t.Fatalf("unexpected rule group ARN %q", arn)
		}
	}
}

func TestListRuleGroupsUsesMetadataReadAndTags(t *testing.T) {
	fake := &fakeAPIClient{
		ruleGroupPages: [][]awsnetfwtypes.RuleGroupMetadata{
			{{Arn: aws.String("arn:rg:1"), Name: aws.String("rg1")}},
		},
		ruleGroupMetadata: map[string]*awsnetfw.DescribeRuleGroupMetadataOutput{
			"arn:rg:1": {
				RuleGroupArn:  aws.String("arn:rg:1"),
				RuleGroupName: aws.String("rg1"),
				Type:          awsnetfwtypes.RuleGroupTypeStateful,
				Capacity:      aws.Int32(1000),
				Description:   aws.String("threats"),
			},
		},
		tags: map[string][]awsnetfwtypes.Tag{
			"arn:rg:1": {{Key: aws.String("team"), Value: aws.String("sec")}},
		},
	}
	client := newTestClient(fake)

	ruleGroups, err := client.ListRuleGroups(context.Background())
	if err != nil {
		t.Fatalf("ListRuleGroups() error = %v", err)
	}
	if len(ruleGroups) != 1 {
		t.Fatalf("ListRuleGroups() returned %d, want 1", len(ruleGroups))
	}
	ruleGroup := ruleGroups[0]
	if ruleGroup.Type != "STATEFUL" || ruleGroup.Capacity != 1000 {
		t.Fatalf("rule group mapped incorrectly: %#v", ruleGroup)
	}
	if ruleGroup.Tags["team"] != "sec" {
		t.Fatalf("rule group tags = %#v, want team=sec", ruleGroup.Tags)
	}
	if fake.describeRuleGroupCalls != 0 {
		t.Fatalf("adapter called DescribeRuleGroup %d times; it must use DescribeRuleGroupMetadata only", fake.describeRuleGroupCalls)
	}
	if fake.describeRuleGroupMetadataCalls != 1 {
		t.Fatalf("adapter called DescribeRuleGroupMetadata %d times, want 1", fake.describeRuleGroupMetadataCalls)
	}
}

func TestListTLSInspectionConfigurationsMapsMetadata(t *testing.T) {
	fake := &fakeAPIClient{
		tlsPages: [][]awsnetfwtypes.TLSInspectionConfigurationMetadata{
			{{Arn: aws.String("arn:tls:1"), Name: aws.String("tls1")}},
		},
		tlsConfigs: map[string]*awsnetfw.DescribeTLSInspectionConfigurationOutput{
			"arn:tls:1": {
				TLSInspectionConfigurationResponse: &awsnetfwtypes.TLSInspectionConfigurationResponse{
					TLSInspectionConfigurationArn:    aws.String("arn:tls:1"),
					TLSInspectionConfigurationId:     aws.String("tlsid1"),
					TLSInspectionConfigurationName:   aws.String("tls1"),
					TLSInspectionConfigurationStatus: awsnetfwtypes.ResourceStatusActive,
					NumberOfAssociations:             aws.Int32(1),
				},
			},
		},
	}
	client := newTestClient(fake)

	tlsConfigs, err := client.ListTLSInspectionConfigurations(context.Background())
	if err != nil {
		t.Fatalf("ListTLSInspectionConfigurations() error = %v", err)
	}
	if len(tlsConfigs) != 1 {
		t.Fatalf("ListTLSInspectionConfigurations() returned %d, want 1", len(tlsConfigs))
	}
	if tlsConfigs[0].Status != "ACTIVE" || tlsConfigs[0].NumberOfAssociations != 1 {
		t.Fatalf("TLS config mapped incorrectly: %#v", tlsConfigs[0])
	}
}

// TestScannerOwnedTypesDeclareNoRuleBodyField proves the scanner-owned domain
// types cannot carry a rule source, Suricata signature body, certificate body,
// or full policy rule body, even if a future adapter edit tried to populate
// one. The field-name guard is a structural complement to the SDK adapter's
// interface exclusion gate.
func TestScannerOwnedTypesDeclareNoRuleBodyField(t *testing.T) {
	// These substrings name rule sources, Suricata signature bodies,
	// certificate material, or TLS scope rule bodies. Aggregate counts
	// (ConsumedStatefulRuleCapacity) and reference ARNs (RuleGroupARNs) are
	// metadata and use the broader "rule" token, so the guard targets the
	// body-bearing tokens directly instead.
	forbiddenSubstrings := []string{
		"rulesource", "rulebody", "rulestring", "suricata", "signature",
		"statement", "certificate", "privatekey", "secret", "sslcontext",
		"scopeconfiguration",
	}
	allowedExact := map[string]struct{}{}
	types := []reflect.Type{
		reflect.TypeOf(netfwservice.Firewall{}),
		reflect.TypeOf(netfwservice.FirewallPolicy{}),
		reflect.TypeOf(netfwservice.RuleGroup{}),
		reflect.TypeOf(netfwservice.TLSInspectionConfiguration{}),
	}
	for _, typ := range types {
		for i := 0; i < typ.NumField(); i++ {
			lower := strings.ToLower(typ.Field(i).Name)
			if _, ok := allowedExact[lower]; ok {
				continue
			}
			for _, forbidden := range forbiddenSubstrings {
				if strings.Contains(lower, forbidden) {
					t.Fatalf("%s.%s may carry a rule/secret body; scanner-owned types must hold metadata only",
						typ.Name(), typ.Field(i).Name)
				}
			}
		}
	}
}

func newTestClient(fake *fakeAPIClient) *Client {
	return &Client{
		client: fake,
		boundary: awscloud.Boundary{
			AccountID:   "123456789012",
			Region:      "us-east-1",
			ServiceKind: awscloud.ServiceNetworkFirewall,
		},
	}
}

type fakeAPIClient struct {
	firewallPages [][]awsnetfwtypes.FirewallMetadata
	firewalls     map[string]*awsnetfw.DescribeFirewallOutput

	policyPages [][]awsnetfwtypes.FirewallPolicyMetadata
	policies    map[string]*awsnetfw.DescribeFirewallPolicyOutput

	ruleGroupPages    [][]awsnetfwtypes.RuleGroupMetadata
	ruleGroupMetadata map[string]*awsnetfw.DescribeRuleGroupMetadataOutput

	tlsPages   [][]awsnetfwtypes.TLSInspectionConfigurationMetadata
	tlsConfigs map[string]*awsnetfw.DescribeTLSInspectionConfigurationOutput

	tags map[string][]awsnetfwtypes.Tag

	describeRuleGroupCalls         int
	describeRuleGroupMetadataCalls int
}

func pageToken(index int) *string {
	return aws.String("page-" + strconv.Itoa(index))
}

func tokenIndex(token *string) int {
	idx, _ := strconv.Atoi(strings.TrimPrefix(aws.ToString(token), "page-"))
	return idx
}

func (f *fakeAPIClient) ListFirewalls(_ context.Context, in *awsnetfw.ListFirewallsInput, _ ...func(*awsnetfw.Options)) (*awsnetfw.ListFirewallsOutput, error) {
	idx := 0
	if in.NextToken != nil {
		idx = tokenIndex(in.NextToken)
	}
	out := &awsnetfw.ListFirewallsOutput{Firewalls: f.firewallPages[idx]}
	if idx+1 < len(f.firewallPages) {
		out.NextToken = pageToken(idx + 1)
	}
	return out, nil
}

func (f *fakeAPIClient) DescribeFirewall(_ context.Context, in *awsnetfw.DescribeFirewallInput, _ ...func(*awsnetfw.Options)) (*awsnetfw.DescribeFirewallOutput, error) {
	return f.firewalls[aws.ToString(in.FirewallArn)], nil
}

func (f *fakeAPIClient) ListFirewallPolicies(_ context.Context, in *awsnetfw.ListFirewallPoliciesInput, _ ...func(*awsnetfw.Options)) (*awsnetfw.ListFirewallPoliciesOutput, error) {
	idx := 0
	if in.NextToken != nil {
		idx = tokenIndex(in.NextToken)
	}
	out := &awsnetfw.ListFirewallPoliciesOutput{FirewallPolicies: f.policyPages[idx]}
	if idx+1 < len(f.policyPages) {
		out.NextToken = pageToken(idx + 1)
	}
	return out, nil
}

func (f *fakeAPIClient) DescribeFirewallPolicy(_ context.Context, in *awsnetfw.DescribeFirewallPolicyInput, _ ...func(*awsnetfw.Options)) (*awsnetfw.DescribeFirewallPolicyOutput, error) {
	return f.policies[aws.ToString(in.FirewallPolicyArn)], nil
}

func (f *fakeAPIClient) ListRuleGroups(_ context.Context, in *awsnetfw.ListRuleGroupsInput, _ ...func(*awsnetfw.Options)) (*awsnetfw.ListRuleGroupsOutput, error) {
	idx := 0
	if in.NextToken != nil {
		idx = tokenIndex(in.NextToken)
	}
	out := &awsnetfw.ListRuleGroupsOutput{RuleGroups: f.ruleGroupPages[idx]}
	if idx+1 < len(f.ruleGroupPages) {
		out.NextToken = pageToken(idx + 1)
	}
	return out, nil
}

func (f *fakeAPIClient) DescribeRuleGroupMetadata(_ context.Context, in *awsnetfw.DescribeRuleGroupMetadataInput, _ ...func(*awsnetfw.Options)) (*awsnetfw.DescribeRuleGroupMetadataOutput, error) {
	f.describeRuleGroupMetadataCalls++
	return f.ruleGroupMetadata[aws.ToString(in.RuleGroupArn)], nil
}

func (f *fakeAPIClient) ListTLSInspectionConfigurations(_ context.Context, in *awsnetfw.ListTLSInspectionConfigurationsInput, _ ...func(*awsnetfw.Options)) (*awsnetfw.ListTLSInspectionConfigurationsOutput, error) {
	idx := 0
	if in.NextToken != nil {
		idx = tokenIndex(in.NextToken)
	}
	out := &awsnetfw.ListTLSInspectionConfigurationsOutput{TLSInspectionConfigurations: f.tlsPages[idx]}
	if idx+1 < len(f.tlsPages) {
		out.NextToken = pageToken(idx + 1)
	}
	return out, nil
}

func (f *fakeAPIClient) DescribeTLSInspectionConfiguration(_ context.Context, in *awsnetfw.DescribeTLSInspectionConfigurationInput, _ ...func(*awsnetfw.Options)) (*awsnetfw.DescribeTLSInspectionConfigurationOutput, error) {
	return f.tlsConfigs[aws.ToString(in.TLSInspectionConfigurationArn)], nil
}

func (f *fakeAPIClient) ListTagsForResource(_ context.Context, in *awsnetfw.ListTagsForResourceInput, _ ...func(*awsnetfw.Options)) (*awsnetfw.ListTagsForResourceOutput, error) {
	return &awsnetfw.ListTagsForResourceOutput{Tags: f.tags[aws.ToString(in.ResourceArn)]}, nil
}
