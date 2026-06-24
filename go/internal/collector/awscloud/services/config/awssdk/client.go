// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/service/configservice"
	cfgtypes "github.com/aws/aws-sdk-go-v2/service/configservice/types"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	configservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/config"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// apiClient is the minimal AWS SDK for Go v2 AWS Config surface the adapter
// consumes. It is intentionally read-only and metadata-only: it describes
// configuration recorders, delivery channels, config rules, conformance packs
// (with status and member-rule compliance for the rule count), configuration
// aggregators, and retention configurations. It exposes no recorded
// configuration-item read, no per-resource compliance-detail read, no
// custom-rule policy-body read, and no mutation API. The reflection gate in
// client_test.go enforces this.
type apiClient interface {
	DescribeConfigurationRecorders(context.Context, *awsconfig.DescribeConfigurationRecordersInput, ...func(*awsconfig.Options)) (*awsconfig.DescribeConfigurationRecordersOutput, error)
	DescribeDeliveryChannels(context.Context, *awsconfig.DescribeDeliveryChannelsInput, ...func(*awsconfig.Options)) (*awsconfig.DescribeDeliveryChannelsOutput, error)
	DescribeConfigRules(context.Context, *awsconfig.DescribeConfigRulesInput, ...func(*awsconfig.Options)) (*awsconfig.DescribeConfigRulesOutput, error)
	DescribeConformancePacks(context.Context, *awsconfig.DescribeConformancePacksInput, ...func(*awsconfig.Options)) (*awsconfig.DescribeConformancePacksOutput, error)
	DescribeConformancePackStatus(context.Context, *awsconfig.DescribeConformancePackStatusInput, ...func(*awsconfig.Options)) (*awsconfig.DescribeConformancePackStatusOutput, error)
	DescribeConformancePackCompliance(context.Context, *awsconfig.DescribeConformancePackComplianceInput, ...func(*awsconfig.Options)) (*awsconfig.DescribeConformancePackComplianceOutput, error)
	DescribeConfigurationAggregators(context.Context, *awsconfig.DescribeConfigurationAggregatorsInput, ...func(*awsconfig.Options)) (*awsconfig.DescribeConfigurationAggregatorsOutput, error)
	DescribeRetentionConfigurations(context.Context, *awsconfig.DescribeRetentionConfigurationsInput, ...func(*awsconfig.Options)) (*awsconfig.DescribeRetentionConfigurationsOutput, error)
}

// Client adapts AWS SDK Config control-plane calls into metadata-only scanner
// records.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds an AWS Config SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awsconfig.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// ConfigurationRecorders returns the Config configuration recorders for the
// claimed account and region. The operation is not paginated.
func (c *Client) ConfigurationRecorders(ctx context.Context) ([]configservice.ConfigurationRecorder, error) {
	var output *awsconfig.DescribeConfigurationRecordersOutput
	err := c.recordAPICall(ctx, "DescribeConfigurationRecorders", func(callCtx context.Context) error {
		var err error
		output, err = c.client.DescribeConfigurationRecorders(callCtx, &awsconfig.DescribeConfigurationRecordersInput{})
		return err
	})
	if err != nil {
		return nil, err
	}
	if output == nil {
		return nil, nil
	}
	recorders := make([]configservice.ConfigurationRecorder, 0, len(output.ConfigurationRecorders))
	for _, recorder := range output.ConfigurationRecorders {
		recorders = append(recorders, mapRecorder(recorder))
	}
	return recorders, nil
}

// DeliveryChannels returns the Config delivery channels for the claimed account
// and region. The operation is not paginated.
func (c *Client) DeliveryChannels(ctx context.Context) ([]configservice.DeliveryChannel, error) {
	var output *awsconfig.DescribeDeliveryChannelsOutput
	err := c.recordAPICall(ctx, "DescribeDeliveryChannels", func(callCtx context.Context) error {
		var err error
		output, err = c.client.DescribeDeliveryChannels(callCtx, &awsconfig.DescribeDeliveryChannelsInput{})
		return err
	})
	if err != nil {
		return nil, err
	}
	if output == nil {
		return nil, nil
	}
	channels := make([]configservice.DeliveryChannel, 0, len(output.DeliveryChannels))
	for _, channel := range output.DeliveryChannels {
		channels = append(channels, mapDeliveryChannel(channel))
	}
	return channels, nil
}

// ConfigRules returns Config rule metadata. For a CUSTOM_LAMBDA rule the
// Source.SourceIdentifier field carries the evaluator Lambda function ARN, which
// the adapter maps into LambdaFunctionARN; for managed rules it stays in
// SourceIdentifier. The adapter never reads compliance evaluation result
// bodies.
func (c *Client) ConfigRules(ctx context.Context) ([]configservice.ConfigRule, error) {
	var rules []configservice.ConfigRule
	var nextToken *string
	for {
		var page *awsconfig.DescribeConfigRulesOutput
		err := c.recordAPICall(ctx, "DescribeConfigRules", func(callCtx context.Context) error {
			var err error
			page, err = c.client.DescribeConfigRules(callCtx, &awsconfig.DescribeConfigRulesInput{NextToken: nextToken})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return rules, nil
		}
		for _, rule := range page.ConfigRules {
			rules = append(rules, mapConfigRule(rule))
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return rules, nil
		}
	}
}

// ConformancePacks returns conformance pack metadata. The adapter joins the
// pack detail with its deployment status (DescribeConformancePackStatus) and
// the member-rule names from DescribeConformancePackCompliance, which reports
// aggregate per-rule compliance. It never reads conformance pack template
// bodies or per-resource compliance detail.
func (c *Client) ConformancePacks(ctx context.Context) ([]configservice.ConformancePack, error) {
	details, err := c.describeConformancePackDetails(ctx)
	if err != nil {
		return nil, err
	}
	statusByName, err := c.describeConformancePackStatus(ctx)
	if err != nil {
		return nil, err
	}
	packs := make([]configservice.ConformancePack, 0, len(details))
	for _, detail := range details {
		name := strings.TrimSpace(aws.ToString(detail.ConformancePackName))
		ruleNames, err := c.describeConformancePackRuleNames(ctx, name)
		if err != nil {
			return nil, err
		}
		packs = append(packs, configservice.ConformancePack{
			Name:      name,
			ARN:       strings.TrimSpace(aws.ToString(detail.ConformancePackArn)),
			ID:        strings.TrimSpace(aws.ToString(detail.ConformancePackId)),
			Status:    statusByName[name],
			CreatedBy: strings.TrimSpace(aws.ToString(detail.CreatedBy)),
			RuleNames: ruleNames,
		})
	}
	return packs, nil
}

func (c *Client) describeConformancePackDetails(ctx context.Context) ([]cfgtypes.ConformancePackDetail, error) {
	var details []cfgtypes.ConformancePackDetail
	var nextToken *string
	for {
		var page *awsconfig.DescribeConformancePacksOutput
		err := c.recordAPICall(ctx, "DescribeConformancePacks", func(callCtx context.Context) error {
			var err error
			page, err = c.client.DescribeConformancePacks(callCtx, &awsconfig.DescribeConformancePacksInput{NextToken: nextToken})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return details, nil
		}
		details = append(details, page.ConformancePackDetails...)
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return details, nil
		}
	}
}

func (c *Client) describeConformancePackStatus(ctx context.Context) (map[string]string, error) {
	statusByName := map[string]string{}
	var nextToken *string
	for {
		var page *awsconfig.DescribeConformancePackStatusOutput
		err := c.recordAPICall(ctx, "DescribeConformancePackStatus", func(callCtx context.Context) error {
			var err error
			page, err = c.client.DescribeConformancePackStatus(callCtx, &awsconfig.DescribeConformancePackStatusInput{NextToken: nextToken})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return statusByName, nil
		}
		for _, status := range page.ConformancePackStatusDetails {
			name := strings.TrimSpace(aws.ToString(status.ConformancePackName))
			if name != "" {
				statusByName[name] = string(status.ConformancePackState)
			}
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return statusByName, nil
		}
	}
}

func (c *Client) describeConformancePackRuleNames(ctx context.Context, packName string) ([]string, error) {
	if packName == "" {
		return nil, nil
	}
	var ruleNames []string
	seen := map[string]struct{}{}
	var nextToken *string
	for {
		var page *awsconfig.DescribeConformancePackComplianceOutput
		err := c.recordAPICall(ctx, "DescribeConformancePackCompliance", func(callCtx context.Context) error {
			var err error
			page, err = c.client.DescribeConformancePackCompliance(callCtx, &awsconfig.DescribeConformancePackComplianceInput{
				ConformancePackName: aws.String(packName),
				NextToken:           nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return ruleNames, nil
		}
		for _, rule := range page.ConformancePackRuleComplianceList {
			name := strings.TrimSpace(aws.ToString(rule.ConfigRuleName))
			if name == "" {
				continue
			}
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			ruleNames = append(ruleNames, name)
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return ruleNames, nil
		}
	}
}

// ConfigurationAggregators returns Config configuration aggregator metadata with
// the aggregated source account and region set.
func (c *Client) ConfigurationAggregators(ctx context.Context) ([]configservice.ConfigurationAggregator, error) {
	var aggregators []configservice.ConfigurationAggregator
	var nextToken *string
	for {
		var page *awsconfig.DescribeConfigurationAggregatorsOutput
		err := c.recordAPICall(ctx, "DescribeConfigurationAggregators", func(callCtx context.Context) error {
			var err error
			page, err = c.client.DescribeConfigurationAggregators(callCtx, &awsconfig.DescribeConfigurationAggregatorsInput{NextToken: nextToken})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return aggregators, nil
		}
		for _, aggregator := range page.ConfigurationAggregators {
			aggregators = append(aggregators, mapAggregator(aggregator))
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return aggregators, nil
		}
	}
}

// RetentionConfigurations returns Config retention configuration metadata.
func (c *Client) RetentionConfigurations(ctx context.Context) ([]configservice.RetentionConfiguration, error) {
	var retentions []configservice.RetentionConfiguration
	var nextToken *string
	for {
		var page *awsconfig.DescribeRetentionConfigurationsOutput
		err := c.recordAPICall(ctx, "DescribeRetentionConfigurations", func(callCtx context.Context) error {
			var err error
			page, err = c.client.DescribeRetentionConfigurations(callCtx, &awsconfig.DescribeRetentionConfigurationsInput{NextToken: nextToken})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return retentions, nil
		}
		for _, retention := range page.RetentionConfigurations {
			retentions = append(retentions, configservice.RetentionConfiguration{
				Name:                  strings.TrimSpace(aws.ToString(retention.Name)),
				RetentionPeriodInDays: aws.ToInt32(retention.RetentionPeriodInDays),
			})
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return retentions, nil
		}
	}
}

func mapRecorder(recorder cfgtypes.ConfigurationRecorder) configservice.ConfigurationRecorder {
	mapped := configservice.ConfigurationRecorder{
		Name: strings.TrimSpace(aws.ToString(recorder.Name)),
	}
	group := recorder.RecordingGroup
	if group == nil {
		return mapped
	}
	mapped.AllSupported = group.AllSupported
	mapped.IncludeGlobalResourceTypes = group.IncludeGlobalResourceTypes
	mapped.ResourceTypes = mapResourceTypes(group.ResourceTypes)
	if group.RecordingStrategy != nil {
		mapped.RecordingStrategy = string(group.RecordingStrategy.UseOnly)
	}
	return mapped
}

func mapResourceTypes(types []cfgtypes.ResourceType) []string {
	if len(types) == 0 {
		return nil
	}
	output := make([]string, 0, len(types))
	for _, resourceType := range types {
		if trimmed := strings.TrimSpace(string(resourceType)); trimmed != "" {
			output = append(output, trimmed)
		}
	}
	if len(output) == 0 {
		return nil
	}
	return output
}

func mapDeliveryChannel(channel cfgtypes.DeliveryChannel) configservice.DeliveryChannel {
	mapped := configservice.DeliveryChannel{
		Name:         strings.TrimSpace(aws.ToString(channel.Name)),
		S3BucketName: strings.TrimSpace(aws.ToString(channel.S3BucketName)),
		S3KeyPrefix:  strings.TrimSpace(aws.ToString(channel.S3KeyPrefix)),
		S3KMSKeyARN:  strings.TrimSpace(aws.ToString(channel.S3KmsKeyArn)),
		SNSTopicARN:  strings.TrimSpace(aws.ToString(channel.SnsTopicARN)),
	}
	if channel.ConfigSnapshotDeliveryProperties != nil {
		mapped.SnapshotDeliveryInterval = string(channel.ConfigSnapshotDeliveryProperties.DeliveryFrequency)
	}
	return mapped
}

func mapConfigRule(rule cfgtypes.ConfigRule) configservice.ConfigRule {
	mapped := configservice.ConfigRule{
		Name:  strings.TrimSpace(aws.ToString(rule.ConfigRuleName)),
		ARN:   strings.TrimSpace(aws.ToString(rule.ConfigRuleArn)),
		ID:    strings.TrimSpace(aws.ToString(rule.ConfigRuleId)),
		State: string(rule.ConfigRuleState),
	}
	if rule.Source != nil {
		mapped.Owner = string(rule.Source.Owner)
		identifier := strings.TrimSpace(aws.ToString(rule.Source.SourceIdentifier))
		// For a CUSTOM_LAMBDA rule, AWS Config stores the evaluator Lambda
		// function ARN in SourceIdentifier. Route it to LambdaFunctionARN so the
		// scanner can build the custom-rule-to-Lambda edge; managed and policy
		// rules keep the value in SourceIdentifier.
		if rule.Source.Owner == cfgtypes.OwnerCustomLambda {
			mapped.LambdaFunctionARN = identifier
		} else {
			mapped.SourceIdentifier = identifier
		}
	}
	if rule.Scope != nil {
		mapped.ScopeResourceTypes = trimStrings(rule.Scope.ComplianceResourceTypes)
	}
	return mapped
}

func mapAggregator(aggregator cfgtypes.ConfigurationAggregator) configservice.ConfigurationAggregator {
	mapped := configservice.ConfigurationAggregator{
		Name:      strings.TrimSpace(aws.ToString(aggregator.ConfigurationAggregatorName)),
		ARN:       strings.TrimSpace(aws.ToString(aggregator.ConfigurationAggregatorArn)),
		CreatedBy: strings.TrimSpace(aws.ToString(aggregator.CreatedBy)),
	}
	for _, source := range aggregator.AccountAggregationSources {
		mapped.SourceAccountIDs = append(mapped.SourceAccountIDs, trimStrings(source.AccountIds)...)
		mapped.SourceRegions = append(mapped.SourceRegions, trimStrings(source.AwsRegions)...)
		if source.AllAwsRegions {
			mapped.AllAWSRegions = true
		}
	}
	if org := aggregator.OrganizationAggregationSource; org != nil {
		mapped.OrganizationRoleARN = strings.TrimSpace(aws.ToString(org.RoleArn))
		mapped.OrganizationAllAWSRegions = org.AllAwsRegions
		mapped.SourceRegions = append(mapped.SourceRegions, trimStrings(org.AwsRegions)...)
	}
	return mapped
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
