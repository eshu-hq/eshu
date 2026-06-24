// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/service/configservice"
	cfgtypes "github.com/aws/aws-sdk-go-v2/service/configservice/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	configservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/config"
)

// TestAPIClientInterfaceExcludesConfigItemBodyAndMutationAPIs is the security
// gate for the AWS Config SDK adapter. The scanner contract is metadata-only
// and read-only. The adapter must never expose a recorded configuration-item
// read (GetResourceConfigHistory, BatchGetResourceConfig,
// GetDiscoveredResourceCounts), a per-resource compliance-detail read
// (GetComplianceDetailsByConfigRule, GetComplianceDetailsByResource), a
// custom-rule policy-body read (GetCustomRulePolicy), a stored-query read, or
// any mutation API (Put/Delete/Start/Stop/Tag/Untag). This test reflects over
// the adapter's internal apiClient interface and FAILS on any forbidden method,
// using substring matches so additions like StartConfigurationRecorder or
// PutConfigRule cannot slip past.
func TestAPIClientInterfaceExcludesConfigItemBodyAndMutationAPIs(t *testing.T) {
	forbidden := []string{
		// Recorded configuration-item-body reads (full resource snapshots).
		"GetResourceConfigHistory", "BatchGetResourceConfig",
		"GetAggregateResourceConfig", "BatchGetAggregateResourceConfig",
		"GetDiscoveredResourceCounts", "GetAggregateDiscoveredResourceCounts",
		"ListDiscoveredResources", "ListAggregateDiscoveredResources",
		// Per-resource compliance-detail reads.
		"GetComplianceDetailsByConfigRule", "GetComplianceDetailsByResource",
		"GetAggregateComplianceDetailsByConfigRule",
		"GetResourceEvaluationSummary", "ListResourceEvaluations",
		// Custom-rule policy bodies and stored query bodies.
		"GetCustomRulePolicy", "GetOrganizationCustomRulePolicy", "GetStoredQuery",
		// Mutation surface.
		"Put", "Delete", "Start", "Stop",
		"Tag", "Untag", "Select", "BatchPut",
	}
	iface := reflect.TypeOf((*apiClient)(nil)).Elem()
	for i := 0; i < iface.NumMethod(); i++ {
		method := iface.Method(i)
		for _, banned := range forbidden {
			if strings.Contains(method.Name, banned) {
				t.Fatalf("apiClient exposes method %q containing forbidden operation %q; Config adapter is metadata-only and read-only", method.Name, banned)
			}
		}
	}
}

func TestClientReadsConfigMetadata(t *testing.T) {
	api := &fakeConfigAPI{
		recorders: &awsconfig.DescribeConfigurationRecordersOutput{
			ConfigurationRecorders: []cfgtypes.ConfigurationRecorder{{
				Name: aws.String("default"),
				RecordingGroup: &cfgtypes.RecordingGroup{
					AllSupported:               false,
					IncludeGlobalResourceTypes: true,
					ResourceTypes:              []cfgtypes.ResourceType{cfgtypes.ResourceTypeInstance, cfgtypes.ResourceTypeBucket},
					RecordingStrategy:          &cfgtypes.RecordingStrategy{UseOnly: cfgtypes.RecordingStrategyTypeInclusionByResourceTypes},
				},
			}},
		},
		channels: &awsconfig.DescribeDeliveryChannelsOutput{
			DeliveryChannels: []cfgtypes.DeliveryChannel{{
				Name:         aws.String("default"),
				S3BucketName: aws.String("config-bucket"),
				S3KeyPrefix:  aws.String("prefix"),
				S3KmsKeyArn:  aws.String("arn:aws:kms:us-east-1:123456789012:key/abc"),
				SnsTopicARN:  aws.String("arn:aws:sns:us-east-1:123456789012:topic"),
				ConfigSnapshotDeliveryProperties: &cfgtypes.ConfigSnapshotDeliveryProperties{
					DeliveryFrequency: cfgtypes.MaximumExecutionFrequencyTwentyFourHours,
				},
			}},
		},
		rulePages: []*awsconfig.DescribeConfigRulesOutput{{
			ConfigRules: []cfgtypes.ConfigRule{
				{
					ConfigRuleName:  aws.String("managed-rule"),
					ConfigRuleArn:   aws.String("arn:aws:config:us-east-1:123456789012:config-rule/config-rule-aaaa"),
					ConfigRuleId:    aws.String("config-rule-aaaa"),
					ConfigRuleState: cfgtypes.ConfigRuleStateActive,
					Source: &cfgtypes.Source{
						Owner:            cfgtypes.OwnerAws,
						SourceIdentifier: aws.String("S3_BUCKET_PUBLIC_READ_PROHIBITED"),
					},
					Scope: &cfgtypes.Scope{ComplianceResourceTypes: []string{"AWS::S3::Bucket"}},
				},
				{
					ConfigRuleName:  aws.String("custom-lambda-rule"),
					ConfigRuleArn:   aws.String("arn:aws:config:us-east-1:123456789012:config-rule/config-rule-bbbb"),
					ConfigRuleState: cfgtypes.ConfigRuleStateActive,
					Source: &cfgtypes.Source{
						Owner:            cfgtypes.OwnerCustomLambda,
						SourceIdentifier: aws.String("arn:aws:lambda:us-east-1:123456789012:function:evaluator"),
					},
				},
			},
		}},
		packPages: []*awsconfig.DescribeConformancePacksOutput{{
			ConformancePackDetails: []cfgtypes.ConformancePackDetail{{
				ConformancePackName: aws.String("best-practices"),
				ConformancePackArn:  aws.String("arn:aws:config:us-east-1:123456789012:conformance-pack/best-practices-abc"),
				ConformancePackId:   aws.String("conformance-pack-abc"),
			}},
		}},
		statusPages: []*awsconfig.DescribeConformancePackStatusOutput{{
			ConformancePackStatusDetails: []cfgtypes.ConformancePackStatusDetail{{
				ConformancePackName:  aws.String("best-practices"),
				ConformancePackArn:   aws.String("arn:aws:config:us-east-1:123456789012:conformance-pack/best-practices-abc"),
				ConformancePackId:    aws.String("conformance-pack-abc"),
				ConformancePackState: cfgtypes.ConformancePackStateCreateComplete,
			}},
		}},
		compliancePagesByPack: map[string][]*awsconfig.DescribeConformancePackComplianceOutput{
			"best-practices": {{
				ConformancePackName: aws.String("best-practices"),
				ConformancePackRuleComplianceList: []cfgtypes.ConformancePackRuleCompliance{
					{ConfigRuleName: aws.String("rule-a"), ComplianceType: cfgtypes.ConformancePackComplianceTypeCompliant},
					{ConfigRuleName: aws.String("rule-b"), ComplianceType: cfgtypes.ConformancePackComplianceTypeNonCompliant},
				},
			}},
		},
		aggregatorPages: []*awsconfig.DescribeConfigurationAggregatorsOutput{{
			ConfigurationAggregators: []cfgtypes.ConfigurationAggregator{{
				ConfigurationAggregatorName: aws.String("org-aggregator"),
				ConfigurationAggregatorArn:  aws.String("arn:aws:config:us-east-1:123456789012:config-aggregator/config-aggregator-zzzz"),
				AccountAggregationSources: []cfgtypes.AccountAggregationSource{{
					AccountIds: []string{"111122223333", "444455556666"},
					AwsRegions: []string{"us-east-1"},
				}},
			}},
		}},
		retentionPages: []*awsconfig.DescribeRetentionConfigurationsOutput{{
			RetentionConfigurations: []cfgtypes.RetentionConfiguration{{
				Name:                  aws.String("default"),
				RetentionPeriodInDays: aws.Int32(2557),
			}},
		}},
	}
	adapter := &Client{
		client:   api,
		boundary: awscloud.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: awscloud.ServiceConfig},
	}

	recorders, err := adapter.ConfigurationRecorders(context.Background())
	if err != nil || len(recorders) != 1 {
		t.Fatalf("ConfigurationRecorders() = %#v, err = %v, want 1 recorder", recorders, err)
	}
	if !recorders[0].IncludeGlobalResourceTypes || len(recorders[0].ResourceTypes) != 2 {
		t.Fatalf("recorder = %#v, want IncludeGlobalResourceTypes and 2 resource types", recorders[0])
	}
	if recorders[0].RecordingStrategy != "INCLUSION_BY_RESOURCE_TYPES" {
		t.Fatalf("recorder strategy = %q, want INCLUSION_BY_RESOURCE_TYPES", recorders[0].RecordingStrategy)
	}

	channels, err := adapter.DeliveryChannels(context.Background())
	if err != nil || len(channels) != 1 {
		t.Fatalf("DeliveryChannels() = %#v, err = %v, want 1 channel", channels, err)
	}
	if channels[0].SnapshotDeliveryInterval != "TwentyFour_Hours" {
		t.Fatalf("channel snapshot interval = %q, want TwentyFour_Hours", channels[0].SnapshotDeliveryInterval)
	}

	rules, err := adapter.ConfigRules(context.Background())
	if err != nil || len(rules) != 2 {
		t.Fatalf("ConfigRules() = %#v, err = %v, want 2 rules", rules, err)
	}
	managed := ruleByName(rules, "managed-rule")
	if managed.Owner != "AWS" || managed.SourceIdentifier != "S3_BUCKET_PUBLIC_READ_PROHIBITED" || managed.LambdaFunctionARN != "" {
		t.Fatalf("managed rule = %#v, want AWS owner, source identifier, no lambda arn", managed)
	}
	if !slices.Equal(managed.ScopeResourceTypes, []string{"AWS::S3::Bucket"}) {
		t.Fatalf("managed rule scope = %#v, want [AWS::S3::Bucket]", managed.ScopeResourceTypes)
	}
	custom := ruleByName(rules, "custom-lambda-rule")
	if custom.Owner != "CUSTOM_LAMBDA" || custom.LambdaFunctionARN != "arn:aws:lambda:us-east-1:123456789012:function:evaluator" {
		t.Fatalf("custom rule = %#v, want CUSTOM_LAMBDA owner with lambda arn", custom)
	}

	packs, err := adapter.ConformancePacks(context.Background())
	if err != nil || len(packs) != 1 {
		t.Fatalf("ConformancePacks() = %#v, err = %v, want 1 pack", packs, err)
	}
	if packs[0].Status != "CREATE_COMPLETE" {
		t.Fatalf("pack status = %q, want CREATE_COMPLETE", packs[0].Status)
	}
	if !slices.Equal(packs[0].RuleNames, []string{"rule-a", "rule-b"}) {
		t.Fatalf("pack rule names = %#v, want [rule-a rule-b]", packs[0].RuleNames)
	}

	aggregators, err := adapter.ConfigurationAggregators(context.Background())
	if err != nil || len(aggregators) != 1 {
		t.Fatalf("ConfigurationAggregators() = %#v, err = %v, want 1 aggregator", aggregators, err)
	}
	if !slices.Equal(aggregators[0].SourceAccountIDs, []string{"111122223333", "444455556666"}) {
		t.Fatalf("aggregator source accounts = %#v, want two accounts", aggregators[0].SourceAccountIDs)
	}

	retentions, err := adapter.RetentionConfigurations(context.Background())
	if err != nil || len(retentions) != 1 {
		t.Fatalf("RetentionConfigurations() = %#v, err = %v, want 1 retention", retentions, err)
	}
	if retentions[0].RetentionPeriodInDays != 2557 {
		t.Fatalf("retention days = %d, want 2557", retentions[0].RetentionPeriodInDays)
	}

	for _, forbidden := range []string{"GetResourceConfigHistory", "GetComplianceDetailsByConfigRule", "GetDiscoveredResourceCounts", "BatchGetResourceConfig", "GetCustomRulePolicy"} {
		if slices.Contains(api.calls, forbidden) {
			t.Fatalf("forbidden Config call %s was made; calls=%v", forbidden, api.calls)
		}
	}
}

func ruleByName(rules []configservice.ConfigRule, name string) configservice.ConfigRule {
	for _, rule := range rules {
		if rule.Name == name {
			return rule
		}
	}
	return configservice.ConfigRule{}
}
