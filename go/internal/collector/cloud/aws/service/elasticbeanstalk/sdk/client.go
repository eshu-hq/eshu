// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sdk

import (
	"context"
	"errors"
	"strings"

	awsv2 "github.com/aws/aws-sdk-go-v2/aws"
	awseb "github.com/aws/aws-sdk-go-v2/service/elasticbeanstalk"
	ebtypes "github.com/aws/aws-sdk-go-v2/service/elasticbeanstalk/types"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
	ebservice "github.com/eshu-hq/eshu/go/internal/collector/cloud/aws/service/elasticbeanstalk"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// apiClient is the narrow Elastic Beanstalk SDK surface the adapter accepts. It
// lists only the metadata read operations the scanner needs. Mutation,
// environment rebuild/terminate, CNAME swap, environment-info data-plane, and
// configuration-validation operations are excluded by construction and proven
// absent by a reflective guard test.
type apiClient interface {
	DescribeApplications(context.Context, *awseb.DescribeApplicationsInput, ...func(*awseb.Options)) (*awseb.DescribeApplicationsOutput, error)
	DescribeEnvironments(context.Context, *awseb.DescribeEnvironmentsInput, ...func(*awseb.Options)) (*awseb.DescribeEnvironmentsOutput, error)
	DescribeApplicationVersions(context.Context, *awseb.DescribeApplicationVersionsInput, ...func(*awseb.Options)) (*awseb.DescribeApplicationVersionsOutput, error)
	DescribeEnvironmentResources(context.Context, *awseb.DescribeEnvironmentResourcesInput, ...func(*awseb.Options)) (*awseb.DescribeEnvironmentResourcesOutput, error)
	DescribeConfigurationSettings(context.Context, *awseb.DescribeConfigurationSettingsInput, ...func(*awseb.Options)) (*awseb.DescribeConfigurationSettingsOutput, error)
}

// Client adapts AWS SDK Elastic Beanstalk responses into scanner-owned records.
type Client struct {
	client      apiClient
	boundary    aws.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds an Elastic Beanstalk SDK adapter for one claimed AWS
// boundary.
func NewClient(
	config awsv2.Config,
	boundary aws.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awseb.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// DescribeApplications returns every Elastic Beanstalk application visible to
// the configured AWS credentials. The API returns all applications in one call.
func (c *Client) DescribeApplications(ctx context.Context) ([]ebservice.Application, error) {
	var output *awseb.DescribeApplicationsOutput
	err := c.recordAPICall(ctx, "DescribeApplications", func(callCtx context.Context) error {
		var err error
		output, err = c.client.DescribeApplications(callCtx, &awseb.DescribeApplicationsInput{})
		return err
	})
	if err != nil {
		return nil, err
	}
	if output == nil {
		return nil, nil
	}
	applications := make([]ebservice.Application, 0, len(output.Applications))
	for _, application := range output.Applications {
		applications = append(applications, mapApplication(application))
	}
	return applications, nil
}

// DescribeEnvironments returns every environment visible to the configured AWS
// credentials. The API paginates through a NextToken cursor.
func (c *Client) DescribeEnvironments(ctx context.Context) ([]ebservice.Environment, error) {
	var environments []ebservice.Environment
	var nextToken *string
	for {
		var output *awseb.DescribeEnvironmentsOutput
		err := c.recordAPICall(ctx, "DescribeEnvironments", func(callCtx context.Context) error {
			var err error
			output, err = c.client.DescribeEnvironments(callCtx, &awseb.DescribeEnvironmentsInput{
				NextToken: nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if output == nil {
			break
		}
		for _, environment := range output.Environments {
			environments = append(environments, mapEnvironment(environment))
		}
		nextToken = output.NextToken
		if awsv2.ToString(nextToken) == "" {
			break
		}
	}
	return environments, nil
}

// DescribeApplicationVersions returns application version metadata visible to
// the configured AWS credentials. The API paginates through a NextToken cursor.
func (c *Client) DescribeApplicationVersions(ctx context.Context) ([]ebservice.ApplicationVersion, error) {
	var versions []ebservice.ApplicationVersion
	var nextToken *string
	for {
		var output *awseb.DescribeApplicationVersionsOutput
		err := c.recordAPICall(ctx, "DescribeApplicationVersions", func(callCtx context.Context) error {
			var err error
			output, err = c.client.DescribeApplicationVersions(callCtx, &awseb.DescribeApplicationVersionsInput{
				NextToken: nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if output == nil {
			break
		}
		for _, version := range output.ApplicationVersions {
			versions = append(versions, mapApplicationVersion(version))
		}
		nextToken = output.NextToken
		if awsv2.ToString(nextToken) == "" {
			break
		}
	}
	return versions, nil
}

// DescribeEnvironmentResources returns the concrete AWS resources reported in
// use by one environment, keyed by environment id.
func (c *Client) DescribeEnvironmentResources(
	ctx context.Context,
	environmentID string,
) (ebservice.EnvironmentResources, error) {
	environmentID = strings.TrimSpace(environmentID)
	if environmentID == "" {
		return ebservice.EnvironmentResources{}, nil
	}
	var output *awseb.DescribeEnvironmentResourcesOutput
	err := c.recordAPICall(ctx, "DescribeEnvironmentResources", func(callCtx context.Context) error {
		var err error
		output, err = c.client.DescribeEnvironmentResources(callCtx, &awseb.DescribeEnvironmentResourcesInput{
			EnvironmentId: awsv2.String(environmentID),
		})
		return err
	})
	if err != nil {
		return ebservice.EnvironmentResources{}, err
	}
	if output == nil {
		return ebservice.EnvironmentResources{}, nil
	}
	return mapEnvironmentResources(output.EnvironmentResources), nil
}

// DescribeConfigurationSettings returns the deployed option settings for one
// environment.
func (c *Client) DescribeConfigurationSettings(
	ctx context.Context,
	applicationName, environmentName string,
) ([]ebservice.OptionSetting, error) {
	applicationName = strings.TrimSpace(applicationName)
	environmentName = strings.TrimSpace(environmentName)
	if applicationName == "" || environmentName == "" {
		return nil, nil
	}
	var output *awseb.DescribeConfigurationSettingsOutput
	err := c.recordAPICall(ctx, "DescribeConfigurationSettings", func(callCtx context.Context) error {
		var err error
		output, err = c.client.DescribeConfigurationSettings(callCtx, &awseb.DescribeConfigurationSettingsInput{
			ApplicationName: awsv2.String(applicationName),
			EnvironmentName: awsv2.String(environmentName),
		})
		return err
	})
	if err != nil {
		return nil, err
	}
	if output == nil || len(output.ConfigurationSettings) == 0 {
		return nil, nil
	}
	// DescribeConfigurationSettings returns one settings description per
	// environment; the deployed environment is the only match for an
	// EnvironmentName query.
	return mapOptionSettings(output.ConfigurationSettings[0].OptionSettings), nil
}

func mapApplication(application ebtypes.ApplicationDescription) ebservice.Application {
	return ebservice.Application{
		ARN:                    awsv2.ToString(application.ApplicationArn),
		Name:                   awsv2.ToString(application.ApplicationName),
		Description:            awsv2.ToString(application.Description),
		ConfigurationTemplates: cloneStrings(application.ConfigurationTemplates),
		VersionLabels:          cloneStrings(application.Versions),
		DateCreated:            awsv2.ToTime(application.DateCreated),
		DateUpdated:            awsv2.ToTime(application.DateUpdated),
	}
}

func mapEnvironment(environment ebtypes.EnvironmentDescription) ebservice.Environment {
	mapped := ebservice.Environment{
		ARN:               awsv2.ToString(environment.EnvironmentArn),
		ID:                awsv2.ToString(environment.EnvironmentId),
		Name:              awsv2.ToString(environment.EnvironmentName),
		ApplicationName:   awsv2.ToString(environment.ApplicationName),
		Status:            string(environment.Status),
		Health:            string(environment.Health),
		HealthStatus:      string(environment.HealthStatus),
		PlatformARN:       awsv2.ToString(environment.PlatformArn),
		SolutionStackName: awsv2.ToString(environment.SolutionStackName),
		CNAME:             awsv2.ToString(environment.CNAME),
		EndpointURL:       awsv2.ToString(environment.EndpointURL),
		VersionLabel:      awsv2.ToString(environment.VersionLabel),
		TemplateName:      awsv2.ToString(environment.TemplateName),
		OperationsRole:    awsv2.ToString(environment.OperationsRole),
		DateCreated:       awsv2.ToTime(environment.DateCreated),
		DateUpdated:       awsv2.ToTime(environment.DateUpdated),
	}
	if environment.Tier != nil {
		mapped.TierName = awsv2.ToString(environment.Tier.Name)
		mapped.TierType = awsv2.ToString(environment.Tier.Type)
	}
	return mapped
}

func mapApplicationVersion(version ebtypes.ApplicationVersionDescription) ebservice.ApplicationVersion {
	mapped := ebservice.ApplicationVersion{
		ARN:             awsv2.ToString(version.ApplicationVersionArn),
		ApplicationName: awsv2.ToString(version.ApplicationName),
		VersionLabel:    awsv2.ToString(version.VersionLabel),
		Description:     awsv2.ToString(version.Description),
		Status:          string(version.Status),
		BuildARN:        awsv2.ToString(version.BuildArn),
		DateCreated:     awsv2.ToTime(version.DateCreated),
		DateUpdated:     awsv2.ToTime(version.DateUpdated),
	}
	if version.SourceBundle != nil {
		mapped.SourceS3Bucket = awsv2.ToString(version.SourceBundle.S3Bucket)
		mapped.SourceS3Key = awsv2.ToString(version.SourceBundle.S3Key)
	}
	if version.SourceBuildInformation != nil {
		mapped.SourceRepository = string(version.SourceBuildInformation.SourceRepository)
	}
	return mapped
}

func mapEnvironmentResources(resources *ebtypes.EnvironmentResourceDescription) ebservice.EnvironmentResources {
	if resources == nil {
		return ebservice.EnvironmentResources{}
	}
	mapped := ebservice.EnvironmentResources{}
	for _, group := range resources.AutoScalingGroups {
		if name := strings.TrimSpace(awsv2.ToString(group.Name)); name != "" {
			mapped.AutoScalingGroupNames = append(mapped.AutoScalingGroupNames, name)
		}
	}
	for _, template := range resources.LaunchTemplates {
		if id := strings.TrimSpace(awsv2.ToString(template.Id)); id != "" {
			mapped.LaunchTemplateIDs = append(mapped.LaunchTemplateIDs, id)
		}
	}
	for _, loadBalancer := range resources.LoadBalancers {
		if name := strings.TrimSpace(awsv2.ToString(loadBalancer.Name)); name != "" {
			mapped.LoadBalancerNames = append(mapped.LoadBalancerNames, name)
		}
	}
	return mapped
}

func mapOptionSettings(settings []ebtypes.ConfigurationOptionSetting) []ebservice.OptionSetting {
	if len(settings) == 0 {
		return nil
	}
	output := make([]ebservice.OptionSetting, 0, len(settings))
	for _, setting := range settings {
		output = append(output, ebservice.OptionSetting{
			Namespace:  awsv2.ToString(setting.Namespace),
			OptionName: awsv2.ToString(setting.OptionName),
			Value:      awsv2.ToString(setting.Value),
		})
	}
	return output
}

func cloneStrings(input []string) []string {
	if len(input) == 0 {
		return nil
	}
	output := make([]string, 0, len(input))
	for _, value := range input {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			output = append(output, trimmed)
		}
	}
	if len(output) == 0 {
		return nil
	}
	return output
}

func (c *Client) recordAPICall(ctx context.Context, operation string, call func(context.Context) error) error {
	if c.tracer != nil {
		var span trace.Span
		ctx, span = c.tracer.Start(ctx, telemetry.SpanAWSServicePaginationPage)
		span.SetAttributes(
			telemetry.AttrService(c.boundary.ServiceKind),
			telemetry.AttrAccount(c.boundary.AccountID),
			telemetry.AttrRegion(c.boundary.Region),
			telemetry.AttrOperation(operation),
		)
		defer span.End()
	}
	err := call(ctx)
	result := "success"
	if err != nil {
		result = "error"
	}
	throttled := isThrottleError(err)
	aws.RecordAPICall(ctx, aws.APICallEvent{
		Boundary:  c.boundary,
		Operation: operation,
		Result:    result,
		Throttled: throttled,
	})
	if c.instruments != nil {
		c.instruments.AWSAPICalls.Add(ctx, 1, metric.WithAttributes(
			telemetry.AttrService(c.boundary.ServiceKind),
			telemetry.AttrAccount(c.boundary.AccountID),
			telemetry.AttrRegion(c.boundary.Region),
			telemetry.AttrOperation(operation),
			telemetry.AttrResult(result),
		))
		if throttled {
			c.instruments.AWSThrottles.Add(ctx, 1, metric.WithAttributes(
				telemetry.AttrService(c.boundary.ServiceKind),
				telemetry.AttrAccount(c.boundary.AccountID),
				telemetry.AttrRegion(c.boundary.Region),
			))
		}
	}
	return err
}

func isThrottleError(err error) bool {
	var apiErr smithy.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	code := apiErr.ErrorCode()
	return strings.Contains(strings.ToLower(code), "throttl") ||
		code == "RequestLimitExceeded" ||
		code == "TooManyRequestsException"
}

var _ ebservice.Client = (*Client)(nil)

var _ apiClient = (*awseb.Client)(nil)
