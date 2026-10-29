// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsapigatewayv2 "github.com/aws/aws-sdk-go-v2/service/apigatewayv2"
	awsapigatewayv2types "github.com/aws/aws-sdk-go-v2/service/apigatewayv2/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	apigatewayservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/service/apigateway"
)

type v2APIClient interface {
	GetApis(context.Context, *awsapigatewayv2.GetApisInput, ...func(*awsapigatewayv2.Options)) (*awsapigatewayv2.GetApisOutput, error)
	GetStages(context.Context, *awsapigatewayv2.GetStagesInput, ...func(*awsapigatewayv2.Options)) (*awsapigatewayv2.GetStagesOutput, error)
	GetIntegrations(context.Context, *awsapigatewayv2.GetIntegrationsInput, ...func(*awsapigatewayv2.Options)) (*awsapigatewayv2.GetIntegrationsOutput, error)
	GetDomainNames(context.Context, *awsapigatewayv2.GetDomainNamesInput, ...func(*awsapigatewayv2.Options)) (*awsapigatewayv2.GetDomainNamesOutput, error)
	GetApiMappings(context.Context, *awsapigatewayv2.GetApiMappingsInput, ...func(*awsapigatewayv2.Options)) (*awsapigatewayv2.GetApiMappingsOutput, error)
}

func (c *Client) listV2APIs(
	ctx context.Context,
) ([]apigatewayservice.V2API, []awscloud.WarningObservation, error) {
	var apis []apigatewayservice.V2API
	var warnings []awscloud.WarningObservation
	var token *string
	for {
		var page *awsapigatewayv2.GetApisOutput
		err := c.recordAPICall(ctx, "GetApis", func(callCtx context.Context) error {
			var err error
			page, err = c.v2.GetApis(callCtx, &awsapigatewayv2.GetApisInput{
				MaxResults: aws.String(v2PageLimit),
				NextToken:  token,
			})
			return err
		})
		if isNetworkTimeoutError(ctx, err) {
			warnings = appendWarningOnce(warnings, c.networkTimeoutWarning(
				"GetApis", apigatewayservice.APIKindV2, "v2_apis",
				"HTTP/WebSocket API inventory", "apigateway_v2_apis_timeout",
			))
			return apis, warnings, nil
		}
		if err != nil {
			return nil, nil, err
		}
		if page == nil {
			return apis, warnings, nil
		}
		for _, item := range page.Items {
			api, apiWarnings, err := c.v2APIMetadata(ctx, item)
			if err != nil {
				return nil, nil, err
			}
			warnings = appendWarningOnce(warnings, apiWarnings...)
			apis = append(apis, api)
		}
		token = page.NextToken
		if aws.ToString(token) == "" {
			return apis, warnings, nil
		}
	}
}

func (c *Client) v2APIMetadata(
	ctx context.Context,
	item awsapigatewayv2types.Api,
) (apigatewayservice.V2API, []awscloud.WarningObservation, error) {
	api := mapV2API(item)
	var warnings []awscloud.WarningObservation
	var err error
	api.Stages, warnings, err = c.listV2Stages(ctx, api.ID)
	if err != nil {
		return apigatewayservice.V2API{}, nil, err
	}
	var integrationWarnings []awscloud.WarningObservation
	api.Integrations, integrationWarnings, err = c.listV2Integrations(ctx, api.ID)
	if err != nil {
		return apigatewayservice.V2API{}, nil, err
	}
	warnings = appendWarningOnce(warnings, integrationWarnings...)
	return api, warnings, nil
}

func (c *Client) listV2Stages(
	ctx context.Context,
	apiID string,
) ([]apigatewayservice.Stage, []awscloud.WarningObservation, error) {
	var stages []apigatewayservice.Stage
	var token *string
	for {
		var page *awsapigatewayv2.GetStagesOutput
		err := c.recordAPICall(ctx, "GetStages", func(callCtx context.Context) error {
			var err error
			page, err = c.v2.GetStages(callCtx, &awsapigatewayv2.GetStagesInput{
				ApiId:      aws.String(apiID),
				MaxResults: aws.String(v2PageLimit),
				NextToken:  token,
			})
			return err
		})
		if isNetworkTimeoutError(ctx, err) {
			return nil, []awscloud.WarningObservation{c.networkTimeoutWarning(
				"GetStages", apigatewayservice.APIKindV2, "v2_stages",
				"HTTP/WebSocket stage inventory", "apigateway_v2_stages_timeout",
			)}, nil
		}
		if err != nil {
			return nil, nil, err
		}
		if page == nil {
			return stages, nil, nil
		}
		for _, item := range page.Items {
			stages = append(stages, mapV2Stage(apiID, item))
		}
		token = page.NextToken
		if aws.ToString(token) == "" {
			return stages, nil, nil
		}
	}
}

func (c *Client) listV2Integrations(
	ctx context.Context,
	apiID string,
) ([]apigatewayservice.Integration, []awscloud.WarningObservation, error) {
	var integrations []apigatewayservice.Integration
	var token *string
	for {
		var page *awsapigatewayv2.GetIntegrationsOutput
		err := c.recordAPICall(ctx, "GetIntegrations", func(callCtx context.Context) error {
			var err error
			page, err = c.v2.GetIntegrations(callCtx, &awsapigatewayv2.GetIntegrationsInput{
				ApiId:      aws.String(apiID),
				MaxResults: aws.String(v2PageLimit),
				NextToken:  token,
			})
			return err
		})
		if isNetworkTimeoutError(ctx, err) {
			return nil, []awscloud.WarningObservation{c.networkTimeoutWarning(
				"GetIntegrations", apigatewayservice.APIKindV2, "v2_integrations",
				"HTTP/WebSocket integration metadata", "apigateway_v2_integrations_timeout",
			)}, nil
		}
		if err != nil {
			return nil, nil, err
		}
		if page == nil {
			return integrations, nil, nil
		}
		for _, item := range page.Items {
			integrations = append(integrations, mapV2Integration(apiID, item))
		}
		token = page.NextToken
		if aws.ToString(token) == "" {
			return integrations, nil, nil
		}
	}
}

func (c *Client) listV2Domains(
	ctx context.Context,
) ([]apigatewayservice.DomainName, []awscloud.WarningObservation, error) {
	var domains []apigatewayservice.DomainName
	var warnings []awscloud.WarningObservation
	var token *string
	for {
		var page *awsapigatewayv2.GetDomainNamesOutput
		err := c.recordAPICall(ctx, "GetDomainNames", func(callCtx context.Context) error {
			var err error
			page, err = c.v2.GetDomainNames(callCtx, &awsapigatewayv2.GetDomainNamesInput{
				MaxResults: aws.String(v2PageLimit),
				NextToken:  token,
			})
			return err
		})
		if isNetworkTimeoutError(ctx, err) {
			warnings = appendWarningOnce(warnings, c.networkTimeoutWarning(
				"GetDomainNames", apigatewayservice.APIKindV2, "v2_domains",
				"HTTP/WebSocket custom-domain inventory", "apigateway_v2_domains_timeout",
			))
			return domains, warnings, nil
		}
		if err != nil {
			return nil, nil, err
		}
		if page == nil {
			return domains, warnings, nil
		}
		for _, item := range page.Items {
			domain := mapV2Domain(item)
			mappings, mappingWarnings, err := c.listV2Mappings(ctx, domain.Name)
			if err != nil {
				return nil, nil, err
			}
			warnings = appendWarningOnce(warnings, mappingWarnings...)
			domain.Mappings = mappings
			domains = append(domains, domain)
		}
		token = page.NextToken
		if aws.ToString(token) == "" {
			return domains, warnings, nil
		}
	}
}

func (c *Client) listV2Mappings(
	ctx context.Context,
	domainName string,
) ([]apigatewayservice.Mapping, []awscloud.WarningObservation, error) {
	var mappings []apigatewayservice.Mapping
	var token *string
	for {
		var page *awsapigatewayv2.GetApiMappingsOutput
		err := c.recordAPICall(ctx, "GetApiMappings", func(callCtx context.Context) error {
			var err error
			page, err = c.v2.GetApiMappings(callCtx, &awsapigatewayv2.GetApiMappingsInput{
				DomainName: aws.String(domainName),
				MaxResults: aws.String(v2PageLimit),
				NextToken:  token,
			})
			return err
		})
		if isNetworkTimeoutError(ctx, err) {
			return nil, []awscloud.WarningObservation{c.networkTimeoutWarning(
				"GetApiMappings", apigatewayservice.APIKindV2, "v2_mappings",
				"HTTP/WebSocket domain-mapping inventory", "apigateway_v2_mappings_timeout",
			)}, nil
		}
		if err != nil {
			return nil, nil, err
		}
		if page == nil {
			return mappings, nil, nil
		}
		for _, item := range page.Items {
			mappings = append(mappings, mapV2Mapping(domainName, item))
		}
		token = page.NextToken
		if aws.ToString(token) == "" {
			return mappings, nil, nil
		}
	}
}
