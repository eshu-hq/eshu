// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsapigatewayv2 "github.com/aws/aws-sdk-go-v2/service/apigatewayv2"

	apigatewayv2service "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/apigatewayv2"
)

func (c *Client) listAPIs(ctx context.Context) ([]apigatewayv2service.API, error) {
	var apis []apigatewayv2service.API
	var token *string
	for {
		var page *awsapigatewayv2.GetApisOutput
		err := c.recordAPICall(ctx, "GetApis", func(callCtx context.Context) error {
			var err error
			page, err = c.api.GetApis(callCtx, &awsapigatewayv2.GetApisInput{
				MaxResults: aws.String(pageLimit),
				NextToken:  token,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return apis, nil
		}
		for _, item := range page.Items {
			apis = append(apis, mapAPI(item))
		}
		token = page.NextToken
		if aws.ToString(token) == "" {
			return apis, nil
		}
	}
}

func (c *Client) listStages(ctx context.Context, apiID string) ([]apigatewayv2service.Stage, error) {
	var stages []apigatewayv2service.Stage
	var token *string
	for {
		var page *awsapigatewayv2.GetStagesOutput
		err := c.recordAPICall(ctx, "GetStages", func(callCtx context.Context) error {
			var err error
			page, err = c.api.GetStages(callCtx, &awsapigatewayv2.GetStagesInput{
				ApiId:      aws.String(apiID),
				MaxResults: aws.String(pageLimit),
				NextToken:  token,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return stages, nil
		}
		for _, item := range page.Items {
			stages = append(stages, mapStage(apiID, item))
		}
		token = page.NextToken
		if aws.ToString(token) == "" {
			return stages, nil
		}
	}
}

func (c *Client) listRoutes(ctx context.Context, apiID string) ([]apigatewayv2service.Route, error) {
	var routes []apigatewayv2service.Route
	var token *string
	for {
		var page *awsapigatewayv2.GetRoutesOutput
		err := c.recordAPICall(ctx, "GetRoutes", func(callCtx context.Context) error {
			var err error
			page, err = c.api.GetRoutes(callCtx, &awsapigatewayv2.GetRoutesInput{
				ApiId:      aws.String(apiID),
				MaxResults: aws.String(pageLimit),
				NextToken:  token,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return routes, nil
		}
		for _, item := range page.Items {
			routes = append(routes, mapRoute(apiID, item))
		}
		token = page.NextToken
		if aws.ToString(token) == "" {
			return routes, nil
		}
	}
}

func (c *Client) listIntegrations(ctx context.Context, apiID string) ([]apigatewayv2service.Integration, error) {
	var integrations []apigatewayv2service.Integration
	var token *string
	for {
		var page *awsapigatewayv2.GetIntegrationsOutput
		err := c.recordAPICall(ctx, "GetIntegrations", func(callCtx context.Context) error {
			var err error
			page, err = c.api.GetIntegrations(callCtx, &awsapigatewayv2.GetIntegrationsInput{
				ApiId:      aws.String(apiID),
				MaxResults: aws.String(pageLimit),
				NextToken:  token,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return integrations, nil
		}
		for _, item := range page.Items {
			integrations = append(integrations, mapIntegration(apiID, item))
		}
		token = page.NextToken
		if aws.ToString(token) == "" {
			return integrations, nil
		}
	}
}

func (c *Client) listAuthorizers(ctx context.Context, apiID string) ([]apigatewayv2service.Authorizer, error) {
	var authorizers []apigatewayv2service.Authorizer
	var token *string
	for {
		var page *awsapigatewayv2.GetAuthorizersOutput
		err := c.recordAPICall(ctx, "GetAuthorizers", func(callCtx context.Context) error {
			var err error
			page, err = c.api.GetAuthorizers(callCtx, &awsapigatewayv2.GetAuthorizersInput{
				ApiId:      aws.String(apiID),
				MaxResults: aws.String(pageLimit),
				NextToken:  token,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return authorizers, nil
		}
		for _, item := range page.Items {
			authorizers = append(authorizers, mapAuthorizer(apiID, item))
		}
		token = page.NextToken
		if aws.ToString(token) == "" {
			return authorizers, nil
		}
	}
}

func (c *Client) listVPCLinks(ctx context.Context) ([]apigatewayv2service.VPCLink, error) {
	var links []apigatewayv2service.VPCLink
	var token *string
	for {
		var page *awsapigatewayv2.GetVpcLinksOutput
		err := c.recordAPICall(ctx, "GetVpcLinks", func(callCtx context.Context) error {
			var err error
			page, err = c.api.GetVpcLinks(callCtx, &awsapigatewayv2.GetVpcLinksInput{
				MaxResults: aws.String(pageLimit),
				NextToken:  token,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return links, nil
		}
		for _, item := range page.Items {
			links = append(links, mapVPCLink(item))
		}
		token = page.NextToken
		if aws.ToString(token) == "" {
			return links, nil
		}
	}
}

func (c *Client) listDomains(ctx context.Context) ([]apigatewayv2service.DomainName, error) {
	var domains []apigatewayv2service.DomainName
	var token *string
	for {
		var page *awsapigatewayv2.GetDomainNamesOutput
		err := c.recordAPICall(ctx, "GetDomainNames", func(callCtx context.Context) error {
			var err error
			page, err = c.api.GetDomainNames(callCtx, &awsapigatewayv2.GetDomainNamesInput{
				MaxResults: aws.String(pageLimit),
				NextToken:  token,
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
			domain := mapDomain(item)
			mappings, err := c.listMappings(ctx, domain.Name)
			if err != nil {
				return nil, err
			}
			domain.Mappings = mappings
			domains = append(domains, domain)
		}
		token = page.NextToken
		if aws.ToString(token) == "" {
			return domains, nil
		}
	}
}

func (c *Client) listMappings(ctx context.Context, domainName string) ([]apigatewayv2service.Mapping, error) {
	if domainName == "" {
		return nil, nil
	}
	var mappings []apigatewayv2service.Mapping
	var token *string
	for {
		var page *awsapigatewayv2.GetApiMappingsOutput
		err := c.recordAPICall(ctx, "GetApiMappings", func(callCtx context.Context) error {
			var err error
			page, err = c.api.GetApiMappings(callCtx, &awsapigatewayv2.GetApiMappingsInput{
				DomainName: aws.String(domainName),
				MaxResults: aws.String(pageLimit),
				NextToken:  token,
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
			mappings = append(mappings, mapMapping(domainName, item))
		}
		token = page.NextToken
		if aws.ToString(token) == "" {
			return mappings, nil
		}
	}
}
