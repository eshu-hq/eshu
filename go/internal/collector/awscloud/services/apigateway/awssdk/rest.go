// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsapigateway "github.com/aws/aws-sdk-go-v2/service/apigateway"
	awsapigatewaytypes "github.com/aws/aws-sdk-go-v2/service/apigateway/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	apigatewayservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/apigateway"
)

type restAPIClient interface {
	GetRestApis(context.Context, *awsapigateway.GetRestApisInput, ...func(*awsapigateway.Options)) (*awsapigateway.GetRestApisOutput, error)
	GetStages(context.Context, *awsapigateway.GetStagesInput, ...func(*awsapigateway.Options)) (*awsapigateway.GetStagesOutput, error)
	GetResources(context.Context, *awsapigateway.GetResourcesInput, ...func(*awsapigateway.Options)) (*awsapigateway.GetResourcesOutput, error)
	GetDomainNames(context.Context, *awsapigateway.GetDomainNamesInput, ...func(*awsapigateway.Options)) (*awsapigateway.GetDomainNamesOutput, error)
	GetBasePathMappings(context.Context, *awsapigateway.GetBasePathMappingsInput, ...func(*awsapigateway.Options)) (*awsapigateway.GetBasePathMappingsOutput, error)
}

func (c *Client) listRESTAPIs(
	ctx context.Context,
) ([]apigatewayservice.RESTAPI, []awscloud.WarningObservation, error) {
	var apis []apigatewayservice.RESTAPI
	var warnings []awscloud.WarningObservation
	var position *string
	for {
		var page *awsapigateway.GetRestApisOutput
		err := c.recordAPICall(ctx, "GetRestApis", func(callCtx context.Context) error {
			var err error
			page, err = c.rest.GetRestApis(callCtx, &awsapigateway.GetRestApisInput{
				Limit:    aws.Int32(restPageLimit),
				Position: position,
			})
			return err
		})
		if err != nil {
			return nil, nil, err
		}
		if page == nil {
			return apis, warnings, nil
		}
		for _, item := range page.Items {
			api, apiWarnings, err := c.restAPIMetadata(ctx, item)
			if err != nil {
				return nil, nil, err
			}
			warnings = appendRESTWarningOnce(warnings, apiWarnings...)
			apis = append(apis, api)
		}
		position = page.Position
		if aws.ToString(position) == "" {
			return apis, warnings, nil
		}
	}
}

func (c *Client) restAPIMetadata(
	ctx context.Context,
	item awsapigatewaytypes.RestApi,
) (apigatewayservice.RESTAPI, []awscloud.WarningObservation, error) {
	api := mapRESTAPI(item)
	var err error
	api.Stages, err = c.listRESTStages(ctx, api.ID)
	if err != nil {
		return apigatewayservice.RESTAPI{}, nil, err
	}
	var warnings []awscloud.WarningObservation
	api.Integrations, warnings, err = c.listRESTIntegrations(ctx, api.ID)
	if err != nil {
		return apigatewayservice.RESTAPI{}, nil, err
	}
	return api, warnings, nil
}

func (c *Client) listRESTStages(ctx context.Context, apiID string) ([]apigatewayservice.Stage, error) {
	var page *awsapigateway.GetStagesOutput
	err := c.recordAPICall(ctx, "GetStages", func(callCtx context.Context) error {
		var err error
		page, err = c.rest.GetStages(callCtx, &awsapigateway.GetStagesInput{RestApiId: aws.String(apiID)})
		return err
	})
	if err != nil || page == nil {
		return nil, err
	}
	stages := make([]apigatewayservice.Stage, 0, len(page.Item))
	for _, item := range page.Item {
		stages = append(stages, mapRESTStage(apiID, item))
	}
	return stages, nil
}

func (c *Client) listRESTIntegrations(
	ctx context.Context,
	apiID string,
) ([]apigatewayservice.Integration, []awscloud.WarningObservation, error) {
	var integrations []apigatewayservice.Integration
	var position *string
	for {
		var page *awsapigateway.GetResourcesOutput
		err := c.recordAPICall(ctx, "GetResources", func(callCtx context.Context) error {
			var err error
			page, err = c.rest.GetResources(callCtx, &awsapigateway.GetResourcesInput{
				RestApiId: aws.String(apiID),
				Embed:     []string{"methods"},
				Limit:     aws.Int32(restPageLimit),
				Position:  position,
			})
			return err
		})
		if isThrottleError(err) {
			return nil, []awscloud.WarningObservation{c.restResourcesThrottleWarning()}, nil
		}
		if err != nil {
			return nil, nil, err
		}
		if page == nil {
			return integrations, nil, nil
		}
		for _, resource := range page.Items {
			integrations = append(integrations, mapRESTResourceIntegrations(apiID, resource)...)
		}
		position = page.Position
		if aws.ToString(position) == "" {
			return integrations, nil, nil
		}
	}
}

func appendRESTWarningOnce(
	warnings []awscloud.WarningObservation,
	candidates ...awscloud.WarningObservation,
) []awscloud.WarningObservation {
	for _, candidate := range candidates {
		if candidate.WarningKind != awscloud.WarningThrottleSustained {
			warnings = append(warnings, candidate)
			continue
		}
		seen := false
		for _, warning := range warnings {
			if warning.WarningKind == awscloud.WarningThrottleSustained &&
				warning.ErrorClass == candidate.ErrorClass {
				seen = true
				break
			}
		}
		if !seen {
			warnings = append(warnings, candidate)
		}
	}
	return warnings
}

func (c *Client) restResourcesThrottleWarning() awscloud.WarningObservation {
	return awscloud.WarningObservation{
		Boundary:    c.boundary,
		WarningKind: awscloud.WarningThrottleSustained,
		ErrorClass:  "throttled",
		Message:     "API Gateway GetResources throttled after SDK retries; REST integration metadata omitted for this scan",
		Attributes: map[string]any{
			"api_kind":          apigatewayservice.APIKindREST,
			"operation":         "GetResources",
			"partial_component": "rest_integrations",
		},
		SourceRecordID: "apigateway_rest_integrations_throttled",
	}
}

func (c *Client) listRESTDomains(ctx context.Context) ([]apigatewayservice.DomainName, error) {
	var domains []apigatewayservice.DomainName
	var position *string
	for {
		var page *awsapigateway.GetDomainNamesOutput
		err := c.recordAPICall(ctx, "GetDomainNames", func(callCtx context.Context) error {
			var err error
			page, err = c.rest.GetDomainNames(callCtx, &awsapigateway.GetDomainNamesInput{
				Limit:    aws.Int32(restPageLimit),
				Position: position,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return domains, nil
		}
		for _, item := range page.Items {
			domain := mapRESTDomain(item)
			mappings, err := c.listRESTMappings(ctx, domain.Name)
			if err != nil {
				return nil, err
			}
			domain.Mappings = mappings
			domains = append(domains, domain)
		}
		position = page.Position
		if aws.ToString(position) == "" {
			return domains, nil
		}
	}
}

func (c *Client) listRESTMappings(ctx context.Context, domainName string) ([]apigatewayservice.Mapping, error) {
	var mappings []apigatewayservice.Mapping
	var position *string
	for {
		var page *awsapigateway.GetBasePathMappingsOutput
		err := c.recordAPICall(ctx, "GetBasePathMappings", func(callCtx context.Context) error {
			var err error
			page, err = c.rest.GetBasePathMappings(callCtx, &awsapigateway.GetBasePathMappingsInput{
				DomainName: aws.String(domainName),
				Limit:      aws.Int32(restPageLimit),
				Position:   position,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return mappings, nil
		}
		for _, item := range page.Items {
			mappings = append(mappings, mapRESTMapping(domainName, item))
		}
		position = page.Position
		if aws.ToString(position) == "" {
			return mappings, nil
		}
	}
}
