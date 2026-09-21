// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"reflect"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsxray "github.com/aws/aws-sdk-go-v2/service/xray"
	awsxraytypes "github.com/aws/aws-sdk-go-v2/service/xray/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// TestAPIClientSurfaceIsConfigOnly reflects over the adapter's private
// apiClient interface and asserts it exposes exactly the three X-Ray
// configuration reads and that NO observability-payload read or mutation method
// is reachable. This is the SDK-boundary half of the config-only exclusion
// contract: because the adapter holds an apiClient value, a method absent here
// is unreachable from this package by construction.
func TestAPIClientSurfaceIsConfigOnly(t *testing.T) {
	ifaceType := reflect.TypeOf((*apiClient)(nil)).Elem()

	allowed := map[string]struct{}{
		"GetGroups":           {},
		"GetSamplingRules":    {},
		"GetEncryptionConfig": {},
	}
	if got, want := ifaceType.NumMethod(), len(allowed); got != want {
		var names []string
		for i := 0; i < ifaceType.NumMethod(); i++ {
			names = append(names, ifaceType.Method(i).Name)
		}
		t.Fatalf("apiClient exposes %d methods %v, want exactly %d config reads", got, names, want)
	}
	for i := 0; i < ifaceType.NumMethod(); i++ {
		name := ifaceType.Method(i).Name
		if _, ok := allowed[name]; !ok {
			t.Fatalf("apiClient exposes unexpected method %q; only config reads are allowed", name)
		}
	}

	forbidden := []string{
		"GetTraceSummaries", "BatchGetTraces", "GetTraceGraph", "GetServiceGraph",
		"GetTimeSeriesServiceStatistics", "GetInsight", "GetInsightSummaries",
		"GetInsightEvents", "GetInsightImpactGraph", "GetSamplingTargets",
		"GetSamplingStatisticSummaries", "PutTraceSegments", "PutTelemetryRecords",
		"CreateGroup", "UpdateGroup", "DeleteGroup", "CreateSamplingRule",
		"UpdateSamplingRule", "DeleteSamplingRule", "PutEncryptionConfig",
	}
	for _, name := range forbidden {
		if _, ok := ifaceType.MethodByName(name); ok {
			t.Fatalf("apiClient exposes forbidden method %q", name)
		}
	}
}

// TestClientMapsGroupsRulesAndConfig exercises the adapter against a fake SDK
// surface and asserts it maps configuration only — group filter expressions,
// sampling rule criteria, and the encryption type/status/key reference — with
// pagination across both list reads.
func TestClientMapsGroupsRulesAndConfig(t *testing.T) {
	priority := int32(1000)
	version := int32(1)
	insightsOn := true
	fake := &fakeAPI{
		groupsPages: [][]awsxraytypes.GroupSummary{
			{{
				GroupARN:         aws.String("arn:aws:xray:us-east-1:123456789012:group/orders/abc"),
				GroupName:        aws.String("orders"),
				FilterExpression: aws.String(`service("orders-api")`),
				InsightsConfiguration: &awsxraytypes.InsightsConfiguration{
					InsightsEnabled: &insightsOn,
				},
			}},
			{{
				GroupARN:  aws.String("arn:aws:xray:us-east-1:123456789012:group/Default/def"),
				GroupName: aws.String("Default"),
			}},
		},
		rulesPages: [][]awsxraytypes.SamplingRuleRecord{
			{{SamplingRule: &awsxraytypes.SamplingRule{
				RuleARN:       aws.String("arn:aws:xray:us-east-1:123456789012:sampling-rule/orders-rule"),
				RuleName:      aws.String("orders-rule"),
				Priority:      &priority,
				ReservoirSize: 5,
				FixedRate:     0.1,
				ServiceName:   aws.String("orders-api"),
				ServiceType:   aws.String("AWS::ECS::Container"),
				Host:          aws.String("*"),
				HTTPMethod:    aws.String("*"),
				URLPath:       aws.String("*"),
				Version:       &version,
			}}},
			// A record with no embedded rule must be skipped, not mapped empty.
			{{SamplingRule: nil}},
		},
		config: &awsxraytypes.EncryptionConfig{
			Type:   awsxraytypes.EncryptionTypeKms,
			Status: awsxraytypes.EncryptionStatusActive,
			KeyId:  aws.String("arn:aws:kms:us-east-1:123456789012:key/abc"),
		},
	}
	client := &Client{client: fake, boundary: testBoundary()}
	ctx := context.Background()

	groups, err := client.GetGroups(ctx)
	if err != nil {
		t.Fatalf("GetGroups() error = %v", err)
	}
	if got, want := len(groups), 2; got != want {
		t.Fatalf("groups = %d, want %d (pagination)", got, want)
	}
	if got, want := groups[0].FilterExpression, `service("orders-api")`; got != want {
		t.Fatalf("group filter_expression = %q, want %q", got, want)
	}
	if groups[0].InsightsEnabled == nil || !*groups[0].InsightsEnabled {
		t.Fatalf("group insights_enabled = %v, want true", groups[0].InsightsEnabled)
	}

	rules, err := client.GetSamplingRules(ctx)
	if err != nil {
		t.Fatalf("GetSamplingRules() error = %v", err)
	}
	if got, want := len(rules), 1; got != want {
		t.Fatalf("rules = %d, want %d (nil rule record skipped)", got, want)
	}
	if got, want := rules[0].ServiceName, "orders-api"; got != want {
		t.Fatalf("rule service_name = %q, want %q", got, want)
	}

	cfg, err := client.GetEncryptionConfig(ctx)
	if err != nil {
		t.Fatalf("GetEncryptionConfig() error = %v", err)
	}
	if cfg == nil {
		t.Fatal("GetEncryptionConfig() = nil, want config")
	}
	if got, want := cfg.Type, "KMS"; got != want {
		t.Fatalf("encryption type = %q, want %q", got, want)
	}
	if got, want := cfg.KeyID, "arn:aws:kms:us-east-1:123456789012:key/abc"; got != want {
		t.Fatalf("encryption key id = %q, want %q", got, want)
	}
}

func TestClientHandlesNilEncryptionConfig(t *testing.T) {
	client := &Client{client: &fakeAPI{}, boundary: testBoundary()}
	cfg, err := client.GetEncryptionConfig(context.Background())
	if err != nil {
		t.Fatalf("GetEncryptionConfig() error = %v", err)
	}
	if cfg != nil {
		t.Fatalf("GetEncryptionConfig() = %#v, want nil", cfg)
	}
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceXRay,
	}
}

// tokenIndex decodes the page index a fake encoded into a NextToken, defaulting
// to the first page when the token is absent.
func tokenIndex(token *string) int {
	if token == nil {
		return 0
	}
	switch aws.ToString(token) {
	case "page-1":
		return 1
	case "page-2":
		return 2
	default:
		return 0
	}
}

// nextToken encodes the page index following idx as a fake NextToken.
func nextToken(idx int) string {
	switch idx {
	case 0:
		return "page-1"
	default:
		return "page-2"
	}
}

// fakeAPI is a paginating fake of the X-Ray SDK surface. NextToken is encoded
// as the page index so each list read walks every page.
type fakeAPI struct {
	groupsPages [][]awsxraytypes.GroupSummary
	rulesPages  [][]awsxraytypes.SamplingRuleRecord
	config      *awsxraytypes.EncryptionConfig
}

func (f *fakeAPI) GetGroups(_ context.Context, in *awsxray.GetGroupsInput, _ ...func(*awsxray.Options)) (*awsxray.GetGroupsOutput, error) {
	idx := tokenIndex(in.NextToken)
	out := &awsxray.GetGroupsOutput{Groups: f.groupsPages[idx]}
	if idx+1 < len(f.groupsPages) {
		out.NextToken = aws.String(nextToken(idx))
	}
	return out, nil
}

func (f *fakeAPI) GetSamplingRules(_ context.Context, in *awsxray.GetSamplingRulesInput, _ ...func(*awsxray.Options)) (*awsxray.GetSamplingRulesOutput, error) {
	idx := tokenIndex(in.NextToken)
	out := &awsxray.GetSamplingRulesOutput{SamplingRuleRecords: f.rulesPages[idx]}
	if idx+1 < len(f.rulesPages) {
		out.NextToken = aws.String(nextToken(idx))
	}
	return out, nil
}

func (f *fakeAPI) GetEncryptionConfig(context.Context, *awsxray.GetEncryptionConfigInput, ...func(*awsxray.Options)) (*awsxray.GetEncryptionConfigOutput, error) {
	return &awsxray.GetEncryptionConfigOutput{EncryptionConfig: f.config}, nil
}
