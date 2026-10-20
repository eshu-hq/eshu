// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/service/configservice"
)

// fakeConfigAPI implements the metadata-only apiClient interface and records the
// operation names it served so tests can assert no configuration-item-body,
// compliance-detail, or mutation call was made.
type fakeConfigAPI struct {
	recorders *awsconfig.DescribeConfigurationRecordersOutput
	channels  *awsconfig.DescribeDeliveryChannelsOutput

	rulePages []*awsconfig.DescribeConfigRulesOutput
	ruleCalls int

	packPages []*awsconfig.DescribeConformancePacksOutput
	packCalls int

	statusPages []*awsconfig.DescribeConformancePackStatusOutput
	statusCalls int

	compliancePagesByPack map[string][]*awsconfig.DescribeConformancePackComplianceOutput
	complianceCallsByPack map[string]int

	aggregatorPages []*awsconfig.DescribeConfigurationAggregatorsOutput
	aggregatorCalls int

	retentionPages []*awsconfig.DescribeRetentionConfigurationsOutput
	retentionCalls int

	calls []string
}

func (f *fakeConfigAPI) DescribeConfigurationRecorders(
	_ context.Context, _ *awsconfig.DescribeConfigurationRecordersInput, _ ...func(*awsconfig.Options),
) (*awsconfig.DescribeConfigurationRecordersOutput, error) {
	f.calls = append(f.calls, "DescribeConfigurationRecorders")
	if f.recorders == nil {
		return &awsconfig.DescribeConfigurationRecordersOutput{}, nil
	}
	return f.recorders, nil
}

func (f *fakeConfigAPI) DescribeDeliveryChannels(
	_ context.Context, _ *awsconfig.DescribeDeliveryChannelsInput, _ ...func(*awsconfig.Options),
) (*awsconfig.DescribeDeliveryChannelsOutput, error) {
	f.calls = append(f.calls, "DescribeDeliveryChannels")
	if f.channels == nil {
		return &awsconfig.DescribeDeliveryChannelsOutput{}, nil
	}
	return f.channels, nil
}

func (f *fakeConfigAPI) DescribeConfigRules(
	_ context.Context, _ *awsconfig.DescribeConfigRulesInput, _ ...func(*awsconfig.Options),
) (*awsconfig.DescribeConfigRulesOutput, error) {
	f.calls = append(f.calls, "DescribeConfigRules")
	return nextPage(f.rulePages, &f.ruleCalls, &awsconfig.DescribeConfigRulesOutput{}), nil
}

func (f *fakeConfigAPI) DescribeConformancePacks(
	_ context.Context, _ *awsconfig.DescribeConformancePacksInput, _ ...func(*awsconfig.Options),
) (*awsconfig.DescribeConformancePacksOutput, error) {
	f.calls = append(f.calls, "DescribeConformancePacks")
	return nextPage(f.packPages, &f.packCalls, &awsconfig.DescribeConformancePacksOutput{}), nil
}

func (f *fakeConfigAPI) DescribeConformancePackStatus(
	_ context.Context, _ *awsconfig.DescribeConformancePackStatusInput, _ ...func(*awsconfig.Options),
) (*awsconfig.DescribeConformancePackStatusOutput, error) {
	f.calls = append(f.calls, "DescribeConformancePackStatus")
	return nextPage(f.statusPages, &f.statusCalls, &awsconfig.DescribeConformancePackStatusOutput{}), nil
}

func (f *fakeConfigAPI) DescribeConformancePackCompliance(
	_ context.Context, input *awsconfig.DescribeConformancePackComplianceInput, _ ...func(*awsconfig.Options),
) (*awsconfig.DescribeConformancePackComplianceOutput, error) {
	f.calls = append(f.calls, "DescribeConformancePackCompliance")
	name := aws.ToString(input.ConformancePackName)
	pages := f.compliancePagesByPack[name]
	if f.complianceCallsByPack == nil {
		f.complianceCallsByPack = map[string]int{}
	}
	idx := f.complianceCallsByPack[name]
	if idx >= len(pages) {
		return &awsconfig.DescribeConformancePackComplianceOutput{}, nil
	}
	f.complianceCallsByPack[name] = idx + 1
	return pages[idx], nil
}

func (f *fakeConfigAPI) DescribeConfigurationAggregators(
	_ context.Context, _ *awsconfig.DescribeConfigurationAggregatorsInput, _ ...func(*awsconfig.Options),
) (*awsconfig.DescribeConfigurationAggregatorsOutput, error) {
	f.calls = append(f.calls, "DescribeConfigurationAggregators")
	return nextPage(f.aggregatorPages, &f.aggregatorCalls, &awsconfig.DescribeConfigurationAggregatorsOutput{}), nil
}

func (f *fakeConfigAPI) DescribeRetentionConfigurations(
	_ context.Context, _ *awsconfig.DescribeRetentionConfigurationsInput, _ ...func(*awsconfig.Options),
) (*awsconfig.DescribeRetentionConfigurationsOutput, error) {
	f.calls = append(f.calls, "DescribeRetentionConfigurations")
	return nextPage(f.retentionPages, &f.retentionCalls, &awsconfig.DescribeRetentionConfigurationsOutput{}), nil
}

func nextPage[T any](pages []*T, calls *int, empty *T) *T {
	if *calls >= len(pages) {
		return empty
	}
	page := pages[*calls]
	*calls = *calls + 1
	return page
}
