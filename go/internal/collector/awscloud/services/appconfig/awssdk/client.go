// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsappconfig "github.com/aws/aws-sdk-go-v2/service/appconfig"
	awsappconfigtypes "github.com/aws/aws-sdk-go-v2/service/appconfig/types"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	appconfigservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/appconfig"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// apiClient is the metadata-only subset of the AWS AppConfig API the adapter
// calls. It is deliberately limited to the application, environment,
// configuration-profile, and deployment-strategy list reads plus resource-tag
// reads. It exposes no GetConfiguration, no GetHostedConfigurationVersion, no
// GetLatestConfiguration (data-plane appconfigdata, never imported), no
// deployment Start/Stop, and no Create/Update/Delete mutation, so the adapter
// cannot read configuration content or write AppConfig state. The
// exclusion_test reflects over this interface to enforce that contract at build
// time.
type apiClient interface {
	ListApplications(
		context.Context,
		*awsappconfig.ListApplicationsInput,
		...func(*awsappconfig.Options),
	) (*awsappconfig.ListApplicationsOutput, error)
	ListEnvironments(
		context.Context,
		*awsappconfig.ListEnvironmentsInput,
		...func(*awsappconfig.Options),
	) (*awsappconfig.ListEnvironmentsOutput, error)
	ListConfigurationProfiles(
		context.Context,
		*awsappconfig.ListConfigurationProfilesInput,
		...func(*awsappconfig.Options),
	) (*awsappconfig.ListConfigurationProfilesOutput, error)
	ListDeploymentStrategies(
		context.Context,
		*awsappconfig.ListDeploymentStrategiesInput,
		...func(*awsappconfig.Options),
	) (*awsappconfig.ListDeploymentStrategiesOutput, error)
}

// Client adapts AWS SDK AppConfig control-plane calls into scanner-owned
// metadata. It never reads configuration content, hosted configuration version
// bodies, or freeform/feature-flag values, never starts or stops deployments,
// and never calls a Create/Update/Delete mutation API.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds an AppConfig SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awsappconfig.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// Snapshot returns AppConfig application metadata and the environments and
// configuration profiles under each application, plus the account-level
// deployment strategies visible to the configured AWS credentials.
// Configuration content is never read.
func (c *Client) Snapshot(ctx context.Context) (appconfigservice.Snapshot, error) {
	applications, err := c.listApplications(ctx)
	if err != nil {
		return appconfigservice.Snapshot{}, err
	}
	for i := range applications {
		environments, err := c.listEnvironments(ctx, applications[i].ID)
		if err != nil {
			return appconfigservice.Snapshot{}, err
		}
		applications[i].Environments = environments
		profiles, err := c.listProfiles(ctx, applications[i].ID)
		if err != nil {
			return appconfigservice.Snapshot{}, err
		}
		applications[i].Profiles = profiles
	}
	strategies, err := c.listDeploymentStrategies(ctx)
	if err != nil {
		return appconfigservice.Snapshot{}, err
	}
	return appconfigservice.Snapshot{
		Applications:         applications,
		DeploymentStrategies: strategies,
	}, nil
}

func (c *Client) listApplications(ctx context.Context) ([]appconfigservice.Application, error) {
	var applications []appconfigservice.Application
	var nextToken *string
	for {
		var page *awsappconfig.ListApplicationsOutput
		err := c.recordAPICall(ctx, "ListApplications", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListApplications(callCtx, &awsappconfig.ListApplicationsInput{
				NextToken: nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return applications, nil
		}
		for _, item := range page.Items {
			applications = append(applications, appconfigservice.Application{
				ID:          strings.TrimSpace(aws.ToString(item.Id)),
				Name:        strings.TrimSpace(aws.ToString(item.Name)),
				Description: strings.TrimSpace(aws.ToString(item.Description)),
			})
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return applications, nil
		}
	}
}

func (c *Client) listEnvironments(ctx context.Context, applicationID string) ([]appconfigservice.Environment, error) {
	applicationID = strings.TrimSpace(applicationID)
	if applicationID == "" {
		return nil, nil
	}
	var environments []appconfigservice.Environment
	var nextToken *string
	for {
		var page *awsappconfig.ListEnvironmentsOutput
		err := c.recordAPICall(ctx, "ListEnvironments", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListEnvironments(callCtx, &awsappconfig.ListEnvironmentsInput{
				ApplicationId: aws.String(applicationID),
				NextToken:     nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return environments, nil
		}
		for _, item := range page.Items {
			environments = append(environments, mapEnvironment(item, applicationID))
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return environments, nil
		}
	}
}

func mapEnvironment(item awsappconfigtypes.Environment, applicationID string) appconfigservice.Environment {
	owner := strings.TrimSpace(aws.ToString(item.ApplicationId))
	if owner == "" {
		owner = strings.TrimSpace(applicationID)
	}
	return appconfigservice.Environment{
		ID:            strings.TrimSpace(aws.ToString(item.Id)),
		ApplicationID: owner,
		Name:          strings.TrimSpace(aws.ToString(item.Name)),
		Description:   strings.TrimSpace(aws.ToString(item.Description)),
		State:         strings.TrimSpace(string(item.State)),
		Monitors:      mapMonitors(item.Monitors),
	}
}

func mapMonitors(monitors []awsappconfigtypes.Monitor) []appconfigservice.Monitor {
	if len(monitors) == 0 {
		return nil
	}
	mapped := make([]appconfigservice.Monitor, 0, len(monitors))
	for _, monitor := range monitors {
		alarmARN := strings.TrimSpace(aws.ToString(monitor.AlarmArn))
		if alarmARN == "" {
			continue
		}
		mapped = append(mapped, appconfigservice.Monitor{
			AlarmARN:     alarmARN,
			AlarmRoleARN: strings.TrimSpace(aws.ToString(monitor.AlarmRoleArn)),
		})
	}
	if len(mapped) == 0 {
		return nil
	}
	return mapped
}

func (c *Client) listProfiles(ctx context.Context, applicationID string) ([]appconfigservice.ConfigurationProfile, error) {
	applicationID = strings.TrimSpace(applicationID)
	if applicationID == "" {
		return nil, nil
	}
	var profiles []appconfigservice.ConfigurationProfile
	var nextToken *string
	for {
		var page *awsappconfig.ListConfigurationProfilesOutput
		err := c.recordAPICall(ctx, "ListConfigurationProfiles", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListConfigurationProfiles(callCtx, &awsappconfig.ListConfigurationProfilesInput{
				ApplicationId: aws.String(applicationID),
				NextToken:     nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return profiles, nil
		}
		for _, item := range page.Items {
			profiles = append(profiles, mapProfile(item, applicationID))
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return profiles, nil
		}
	}
}

func mapProfile(item awsappconfigtypes.ConfigurationProfileSummary, applicationID string) appconfigservice.ConfigurationProfile {
	owner := strings.TrimSpace(aws.ToString(item.ApplicationId))
	if owner == "" {
		owner = strings.TrimSpace(applicationID)
	}
	return appconfigservice.ConfigurationProfile{
		ID:             strings.TrimSpace(aws.ToString(item.Id)),
		ApplicationID:  owner,
		Name:           strings.TrimSpace(aws.ToString(item.Name)),
		Type:           strings.TrimSpace(aws.ToString(item.Type)),
		LocationURI:    strings.TrimSpace(aws.ToString(item.LocationUri)),
		ValidatorTypes: validatorTypeNames(item.ValidatorTypes),
	}
}

func validatorTypeNames(types []awsappconfigtypes.ValidatorType) []string {
	if len(types) == 0 {
		return nil
	}
	names := make([]string, 0, len(types))
	for _, validatorType := range types {
		if name := strings.TrimSpace(string(validatorType)); name != "" {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return nil
	}
	return names
}

func (c *Client) listDeploymentStrategies(ctx context.Context) ([]appconfigservice.DeploymentStrategy, error) {
	var strategies []appconfigservice.DeploymentStrategy
	var nextToken *string
	for {
		var page *awsappconfig.ListDeploymentStrategiesOutput
		err := c.recordAPICall(ctx, "ListDeploymentStrategies", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListDeploymentStrategies(callCtx, &awsappconfig.ListDeploymentStrategiesInput{
				NextToken: nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return strategies, nil
		}
		for _, item := range page.Items {
			strategies = append(strategies, appconfigservice.DeploymentStrategy{
				ID:                          strings.TrimSpace(aws.ToString(item.Id)),
				Name:                        strings.TrimSpace(aws.ToString(item.Name)),
				Description:                 strings.TrimSpace(aws.ToString(item.Description)),
				DeploymentDurationInMinutes: item.DeploymentDurationInMinutes,
				FinalBakeTimeInMinutes:      item.FinalBakeTimeInMinutes,
				GrowthFactor:                aws.ToFloat32(item.GrowthFactor),
				GrowthType:                  strings.TrimSpace(string(item.GrowthType)),
				ReplicateTo:                 strings.TrimSpace(string(item.ReplicateTo)),
			})
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return strategies, nil
		}
	}
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
	awscloud.RecordAPICall(ctx, awscloud.APICallEvent{
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

var _ appconfigservice.Client = (*Client)(nil)

var _ apiClient = (*awsappconfig.Client)(nil)
