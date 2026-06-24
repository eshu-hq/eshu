// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsproton "github.com/aws/aws-sdk-go-v2/service/proton"
	awsprotontypes "github.com/aws/aws-sdk-go-v2/service/proton/types"

	protonservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/proton"
)

func (c *Client) listEnvironments(ctx context.Context) ([]protonservice.Environment, error) {
	var environments []protonservice.Environment
	var nextToken *string
	for {
		var page *awsproton.ListEnvironmentsOutput
		err := c.recordAPICall(ctx, "ListEnvironments", func(callCtx context.Context) error {
			var callErr error
			page, callErr = c.client.ListEnvironments(callCtx, &awsproton.ListEnvironmentsInput{NextToken: nextToken})
			return callErr
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return environments, nil
		}
		for _, summary := range page.Environments {
			mapped, mapErr := c.mapEnvironment(ctx, summary)
			if mapErr != nil {
				return nil, mapErr
			}
			environments = append(environments, mapped)
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return environments, nil
		}
	}
}

func (c *Client) mapEnvironment(
	ctx context.Context,
	summary awsprotontypes.EnvironmentSummary,
) (protonservice.Environment, error) {
	arn := strings.TrimSpace(aws.ToString(summary.Arn))
	tags, err := c.listTags(ctx, arn)
	if err != nil {
		return protonservice.Environment{}, err
	}
	return protonservice.Environment{
		ARN:                       arn,
		Name:                      strings.TrimSpace(aws.ToString(summary.Name)),
		TemplateName:              strings.TrimSpace(aws.ToString(summary.TemplateName)),
		TemplateMajorVersion:      strings.TrimSpace(aws.ToString(summary.TemplateMajorVersion)),
		TemplateMinorVersion:      strings.TrimSpace(aws.ToString(summary.TemplateMinorVersion)),
		Provisioning:              strings.TrimSpace(string(summary.Provisioning)),
		DeploymentStatus:          strings.TrimSpace(string(summary.DeploymentStatus)),
		Description:               strings.TrimSpace(aws.ToString(summary.Description)),
		ProtonServiceRoleArn:      strings.TrimSpace(aws.ToString(summary.ProtonServiceRoleArn)),
		EnvironmentAccountID:      strings.TrimSpace(aws.ToString(summary.EnvironmentAccountId)),
		CreatedAt:                 aws.ToTime(summary.CreatedAt),
		LastDeploymentSucceededAt: aws.ToTime(summary.LastDeploymentSucceededAt),
		Tags:                      tags,
	}, nil
}

func (c *Client) listServices(ctx context.Context) ([]protonservice.Service, error) {
	var services []protonservice.Service
	var nextToken *string
	for {
		var page *awsproton.ListServicesOutput
		err := c.recordAPICall(ctx, "ListServices", func(callCtx context.Context) error {
			var callErr error
			page, callErr = c.client.ListServices(callCtx, &awsproton.ListServicesInput{NextToken: nextToken})
			return callErr
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return services, nil
		}
		for _, summary := range page.Services {
			mapped, mapErr := c.mapService(ctx, summary)
			if mapErr != nil {
				return nil, mapErr
			}
			services = append(services, mapped)
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return services, nil
		}
	}
}

// mapService maps a service summary into scanner-owned metadata and enriches it
// with the source-repository linkage from GetService (by reference only). The
// service Spec manifest body and pipeline Spec body returned by GetService are
// never read.
func (c *Client) mapService(
	ctx context.Context,
	summary awsprotontypes.ServiceSummary,
) (protonservice.Service, error) {
	arn := strings.TrimSpace(aws.ToString(summary.Arn))
	name := strings.TrimSpace(aws.ToString(summary.Name))
	tags, err := c.listTags(ctx, arn)
	if err != nil {
		return protonservice.Service{}, err
	}
	service := protonservice.Service{
		ARN:            arn,
		Name:           name,
		TemplateName:   strings.TrimSpace(aws.ToString(summary.TemplateName)),
		Status:         strings.TrimSpace(string(summary.Status)),
		Description:    strings.TrimSpace(aws.ToString(summary.Description)),
		CreatedAt:      aws.ToTime(summary.CreatedAt),
		LastModifiedAt: aws.ToTime(summary.LastModifiedAt),
		Tags:           tags,
	}
	if name == "" {
		return service, nil
	}
	var detail *awsproton.GetServiceOutput
	err = c.recordAPICall(ctx, "GetService", func(callCtx context.Context) error {
		var callErr error
		detail, callErr = c.client.GetService(callCtx, &awsproton.GetServiceInput{Name: aws.String(name)})
		return callErr
	})
	if err != nil {
		return protonservice.Service{}, err
	}
	if detail != nil && detail.Service != nil {
		// Reference-only linkage; Spec/Pipeline.Spec bodies are intentionally
		// never read off the detail.
		service.BranchName = strings.TrimSpace(aws.ToString(detail.Service.BranchName))
		service.RepositoryID = strings.TrimSpace(aws.ToString(detail.Service.RepositoryId))
		service.RepositoryConnectionArn = strings.TrimSpace(aws.ToString(detail.Service.RepositoryConnectionArn))
	}
	return service, nil
}

func (c *Client) listEnvironmentTemplates(ctx context.Context) ([]protonservice.Template, error) {
	var templates []protonservice.Template
	var nextToken *string
	for {
		var page *awsproton.ListEnvironmentTemplatesOutput
		err := c.recordAPICall(ctx, "ListEnvironmentTemplates", func(callCtx context.Context) error {
			var callErr error
			page, callErr = c.client.ListEnvironmentTemplates(callCtx, &awsproton.ListEnvironmentTemplatesInput{NextToken: nextToken})
			return callErr
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return templates, nil
		}
		for _, summary := range page.Templates {
			mapped, mapErr := c.mapEnvironmentTemplate(ctx, summary)
			if mapErr != nil {
				return nil, mapErr
			}
			templates = append(templates, mapped)
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return templates, nil
		}
	}
}

func (c *Client) mapEnvironmentTemplate(
	ctx context.Context,
	summary awsprotontypes.EnvironmentTemplateSummary,
) (protonservice.Template, error) {
	arn := strings.TrimSpace(aws.ToString(summary.Arn))
	tags, err := c.listTags(ctx, arn)
	if err != nil {
		return protonservice.Template{}, err
	}
	return protonservice.Template{
		ARN:                arn,
		Name:               strings.TrimSpace(aws.ToString(summary.Name)),
		DisplayName:        strings.TrimSpace(aws.ToString(summary.DisplayName)),
		Description:        strings.TrimSpace(aws.ToString(summary.Description)),
		Provisioning:       strings.TrimSpace(string(summary.Provisioning)),
		RecommendedVersion: strings.TrimSpace(aws.ToString(summary.RecommendedVersion)),
		CreatedAt:          aws.ToTime(summary.CreatedAt),
		LastModifiedAt:     aws.ToTime(summary.LastModifiedAt),
		Tags:               tags,
	}, nil
}

func (c *Client) listServiceTemplates(ctx context.Context) ([]protonservice.Template, error) {
	var templates []protonservice.Template
	var nextToken *string
	for {
		var page *awsproton.ListServiceTemplatesOutput
		err := c.recordAPICall(ctx, "ListServiceTemplates", func(callCtx context.Context) error {
			var callErr error
			page, callErr = c.client.ListServiceTemplates(callCtx, &awsproton.ListServiceTemplatesInput{NextToken: nextToken})
			return callErr
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return templates, nil
		}
		for _, summary := range page.Templates {
			mapped, mapErr := c.mapServiceTemplate(ctx, summary)
			if mapErr != nil {
				return nil, mapErr
			}
			templates = append(templates, mapped)
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return templates, nil
		}
	}
}

func (c *Client) mapServiceTemplate(
	ctx context.Context,
	summary awsprotontypes.ServiceTemplateSummary,
) (protonservice.Template, error) {
	arn := strings.TrimSpace(aws.ToString(summary.Arn))
	tags, err := c.listTags(ctx, arn)
	if err != nil {
		return protonservice.Template{}, err
	}
	return protonservice.Template{
		ARN:                arn,
		Name:               strings.TrimSpace(aws.ToString(summary.Name)),
		DisplayName:        strings.TrimSpace(aws.ToString(summary.DisplayName)),
		Description:        strings.TrimSpace(aws.ToString(summary.Description)),
		Provisioning:       strings.TrimSpace(string(summary.PipelineProvisioning)),
		RecommendedVersion: strings.TrimSpace(aws.ToString(summary.RecommendedVersion)),
		CreatedAt:          aws.ToTime(summary.CreatedAt),
		LastModifiedAt:     aws.ToTime(summary.LastModifiedAt),
		Tags:               tags,
	}, nil
}

// listServicePlacements reads every service instance in the account (one
// paginated ListServiceInstances call, no per-service filter) and keeps only the
// service-name/environment-name join keys. The instance Spec body and input
// parameter values are never read.
func (c *Client) listServicePlacements(ctx context.Context) ([]protonservice.ServicePlacement, error) {
	var placements []protonservice.ServicePlacement
	var nextToken *string
	for {
		var page *awsproton.ListServiceInstancesOutput
		err := c.recordAPICall(ctx, "ListServiceInstances", func(callCtx context.Context) error {
			var callErr error
			page, callErr = c.client.ListServiceInstances(callCtx, &awsproton.ListServiceInstancesInput{NextToken: nextToken})
			return callErr
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return placements, nil
		}
		for _, instance := range page.ServiceInstances {
			serviceName := strings.TrimSpace(aws.ToString(instance.ServiceName))
			environmentName := strings.TrimSpace(aws.ToString(instance.EnvironmentName))
			if serviceName == "" || environmentName == "" {
				continue
			}
			placements = append(placements, protonservice.ServicePlacement{
				ServiceName:     serviceName,
				EnvironmentName: environmentName,
			})
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return placements, nil
		}
	}
}
